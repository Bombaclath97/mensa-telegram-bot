// Package mtproto enumerates the members of a Telegram supergroup.
//
// # Why this exists at all
//
// The Bot API — which the rest of this bot speaks — CANNOT list a group's members. There is
// no such method: getChatMember answers about one user you already know, getChatAdministrators
// returns only admins, getChatMemberCount returns an integer. That is deliberate, for
// anti-spam reasons.
//
// This matters because groups_associations is only ever written when a JOIN REQUEST is
// approved. When the bot is added to a group that already holds 500 people, not one of them
// ever issued a join request, so all 500 are invisible: the cleanup sweep cannot kick them,
// /elimina cannot remove them, and /profilo shows them no groups.
//
// MTProto — the protocol the official clients and TDLib speak — is a DIFFERENT API, and it
// does have the method. https://core.telegram.org/method/channels.getParticipants states
// plainly: "Both users and bots can use this method", gated on CHAT_ADMIN_REQUIRED.
//
// # This runs INSIDE the bot, not in a migration tool
//
// An earlier design had this as a one-shot cutover tool. That was wrong. A bot being added to
// a populated group is not a migration that happens once — it is a RECURRING product event,
// and it needs enumerating every single time. So MTProto lives here, in the bot.
//
// Verified by experiment: an MTProto session and the Bot API's getUpdates long-poll can run
// CONCURRENTLY on the same bot token with no conflict (12/12 polls returned ok=True while a
// full enumeration ran). No 409, no session eviction.
//
// # Connections are opened on demand
//
// Enumeration happens a handful of times ever — when the bot is promoted in a group. A
// permanent second connection to Telegram, idling for weeks and needing reconnect/backoff
// handling, would be all cost and no benefit. Each call connects, works, and disconnects.
package mtproto

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Mensa-Italia/mensa-telegram-bot/logger"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

const (
	// pageLimit is the participants-per-request page size. Telegram caps this server-side at
	// 200; asking for more is silently truncated, so the pagination loop keys off what
	// actually came back rather than assuming a full page.
	pageLimit = 200

	// pageDelay throttles pagination. Enumeration is precisely the access pattern Telegram's
	// anti-spam rate limiter watches, and a FLOOD_WAIT mid-sweep would leave a half-read
	// member list — which is worse than not reading it at all (see the completeness contract
	// in ErrIncomplete below).
	pageDelay = 300 * time.Millisecond

	// hardPageCap stops a runaway loop if the offset/emptiness contract ever surprises us.
	// 200 pages * 200 per page = 40k members, far above any Mensa group.
	hardPageCap = 200

	// botAPIChannelBase converts a Bot API chat id to an MTProto channel id.
	//
	// The two APIs number the same chat differently. The Bot API packs every chat type into
	// ONE signed integer space — users positive, legacy groups negative, channels/supergroups
	// prefixed -100 — purely to namespace them. MTProto keeps users, chats and channels in
	// SEPARATE namespaces, so a channel id is always POSITIVE.
	//
	// TDLib: dialog_id = MAX_CHANNEL_ID - channel_id, where MAX_CHANNEL_ID = -1000000000000.
	botAPIChannelBase = 1_000_000_000_000
)

// ErrNotConfigured means TG_API_ID/TG_API_HASH are absent, so enumeration is impossible.
// Callers should degrade rather than fail: the bot works fine without it, it just cannot see
// members who never issued a join request.
var ErrNotConfigured = errors.New("mtproto is not configured (TG_API_ID / TG_API_HASH)")

// ErrParticipantsHidden means the group's admins have hidden the member list, so any
// enumeration would be silently PARTIAL.
//
// This is a hard stop, never a degradation. A partial member list means members who ARE
// verified get classified as unprofiled — and the grace-period kicker would then remove real
// Mensa members. A loud failure is fine; a silent under-count evicts people.
var ErrParticipantsHidden = errors.New("the group's participant list is hidden (can_view_participants=false)")

// Member is one enumerated group member.
type Member struct {
	UserID  int64
	IsBot   bool
	Deleted bool
	// IsAdmin covers both administrators and the creator: neither may be kicked by the
	// grace-period sweep, and the creator cannot be kicked at all.
	IsAdmin bool
}

// Configured reports whether MTProto credentials are present.
//
// Deliberately separate from Enumerate so a caller can decide up front whether to promise a
// group that it will be enumerated, instead of discovering mid-flow that it cannot be.
func Configured() bool {
	return os.Getenv("TG_API_ID") != "" && os.Getenv("TG_API_HASH") != "" && os.Getenv("BOT_TOKEN") != ""
}

// Enumerate returns EVERY member of the given group.
//
// chatID is in the Bot API's format — the negative -100… number the rest of the bot uses, so
// callers never think about the id-space difference.
//
// # The completeness contract
//
// This either returns the FULL member list or an error. It never returns a partial one. Every
// caller uses the result to decide who does NOT belong in the group, so a short list is not a
// smaller truth — it is a wrong answer that gets real members kicked.
func Enumerate(ctx context.Context, chatID int64) ([]Member, error) {
	if !Configured() {
		return nil, ErrNotConfigured
	}

	apiID, err := strconv.Atoi(os.Getenv("TG_API_ID"))
	if err != nil {
		return nil, fmt.Errorf("TG_API_ID must be an integer: %w", err)
	}

	channelID, err := toChannelID(chatID)
	if err != nil {
		return nil, err
	}

	client := telegram.NewClient(apiID, os.Getenv("TG_API_HASH"), telegram.Options{})

	var members []Member
	// client.Run holds the connection open for exactly as long as this callback runs, then
	// tears it down — which IS the on-demand lifecycle, for free.
	err = client.Run(ctx, func(ctx context.Context) error {
		if _, err := client.Auth().Bot(ctx, os.Getenv("BOT_TOKEN")); err != nil {
			return fmt.Errorf("mtproto bot auth: %w", err)
		}

		api := client.API()

		channel, err := resolveChannel(ctx, api, channelID)
		if err != nil {
			return err
		}

		if err := preflight(ctx, api, channel); err != nil {
			return err
		}

		members, err = paginate(ctx, api, channel)
		return err
	})
	if err != nil {
		return nil, err
	}

	return members, nil
}

// resolveChannel turns a bare channel id into an InputChannel carrying the REAL access_hash.
//
// This two-step is NOT optional, and the reason is subtle enough that it cost a debugging
// round to find. https://core.telegram.org/api/peers says a bot "must use" access_hash=0 when
// it has none — and that is true, but only for RESOLUTION. Passing 0 straight to
// channels.getParticipants returns CHANNEL_INVALID; passing it to channels.getChannels
// resolves fine AND hands back the channel's real access_hash, which the participants call
// then accepts.
//
// The zero hash is a key that opens exactly one door: the one that gives you the real key.
//
// This matters beyond tidiness: CHANNEL_INVALID from getParticipants looks identical to
// "Telegram forbids bots from enumerating members", which is a false negative that would sink
// the whole feature.
func resolveChannel(ctx context.Context, api *tg.Client, channelID int64) (*tg.InputChannel, error) {
	res, err := api.ChannelsGetChannels(ctx, []tg.InputChannelClass{
		&tg.InputChannel{ChannelID: channelID, AccessHash: 0},
	})
	if err != nil {
		return nil, fmt.Errorf("resolving channel %d: %w", channelID, interpret(err))
	}

	chats, ok := res.(*tg.MessagesChats)
	if !ok {
		return nil, fmt.Errorf("resolving channel %d: unexpected response %T", channelID, res)
	}

	for _, c := range chats.Chats {
		if ch, ok := c.(*tg.Channel); ok && ch.ID == channelID {
			return &tg.InputChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}, nil
		}
	}

	return nil, fmt.Errorf("channel %d did not resolve: the bot is probably not a member", channelID)
}

// preflight refuses to enumerate a group whose member list we cannot fully see.
//
// can_view_participants is the server telling us whether THIS caller may read the list. If it
// is false, every page below would come back short — and short means verified members get
// classified unprofiled and kicked. Stop here instead.
func preflight(ctx context.Context, api *tg.Client, channel *tg.InputChannel) error {
	full, err := api.ChannelsGetFullChannel(ctx, channel)
	if err != nil {
		return fmt.Errorf("reading channel info: %w", interpret(err))
	}

	cf, ok := full.FullChat.(*tg.ChannelFull)
	if !ok {
		return fmt.Errorf("reading channel info: unexpected type %T", full.FullChat)
	}

	if !cf.CanViewParticipants {
		return ErrParticipantsHidden
	}
	return nil
}

// paginate walks channels.getParticipants to completion.
func paginate(ctx context.Context, api *tg.Client, channel *tg.InputChannel) ([]Member, error) {
	seen := make(map[int64]Member)
	reported := -1
	offset := 0

	for page := 0; ; page++ {
		if page >= hardPageCap {
			return nil, fmt.Errorf("hit the %d-page cap: the pagination contract is not what we assumed", hardPageCap)
		}

		res, err := api.ChannelsGetParticipants(ctx, &tg.ChannelsGetParticipantsRequest{
			Channel: channel,
			// Recent is the plain "everyone" filter. It carries a well-known ~10k practical
			// ceiling; beyond that ChannelParticipantsSearch over letter prefixes is needed.
			// Mensa groups are nowhere near it — and the Count cross-check below would catch
			// us if one ever were.
			Filter: &tg.ChannelParticipantsRecent{},
			Offset: offset,
			Limit:  pageLimit,
			Hash:   0,
		})
		if err != nil {
			return nil, interpret(err)
		}

		participants, ok := res.(*tg.ChannelsChannelParticipants)
		if !ok {
			// ChannelsChannelParticipantsNotModified only occurs with a non-zero Hash, which
			// we never send.
			return nil, fmt.Errorf("unexpected response %T", res)
		}

		if reported < 0 {
			reported = participants.Count
		}

		if len(participants.Participants) == 0 {
			break
		}

		admins := adminIDs(participants.Participants)
		for _, u := range participants.Users {
			user, ok := u.(*tg.User)
			if !ok {
				continue
			}
			_, isAdmin := admins[user.ID]
			seen[user.ID] = Member{
				UserID:  user.ID,
				IsBot:   user.Bot,
				Deleted: user.Deleted,
				IsAdmin: isAdmin,
			}
		}

		offset += len(participants.Participants)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pageDelay):
		}
	}

	members := make([]Member, 0, len(seen))
	for _, m := range seen {
		members = append(members, m)
	}

	// Cross-check against the server's own count. A short read means Telegram stopped giving
	// us rows — rate limiting, the ~10k ceiling, a hidden list the preflight missed. Whatever
	// the cause, an incomplete list must never reach a caller that decides who to kick.
	if reported >= 0 && len(members) < reported {
		return nil, fmt.Errorf("%w: enumerated %d of %d members reported by the server",
			ErrIncomplete, len(members), reported)
	}

	logger.Info("mtproto: enumerated group members", map[string]interface{}{
		"members":  len(members),
		"reported": reported,
	})
	return members, nil
}

// ErrIncomplete means fewer members came back than the server itself reported.
var ErrIncomplete = errors.New("enumeration was incomplete")

// adminIDs collects the user ids of admins and the creator in one page of participants.
func adminIDs(participants []tg.ChannelParticipantClass) map[int64]struct{} {
	admins := make(map[int64]struct{})
	for _, p := range participants {
		switch v := p.(type) {
		case *tg.ChannelParticipantAdmin:
			admins[v.UserID] = struct{}{}
		case *tg.ChannelParticipantCreator:
			admins[v.UserID] = struct{}{}
		}
	}
	return admins
}

// interpret turns the RPC errors that matter into statements of what they mean, because a
// bare "rpc error code 400: CHANNEL_INVALID" reads like "Telegram blocks this" when it
// usually means we skipped a step.
func interpret(err error) error {
	switch {
	case tg.IsChatAdminRequired(err):
		return fmt.Errorf("%w: %v", ErrAdminRequired, err)
	case tg.IsChannelPrivate(err):
		return fmt.Errorf("the bot is not in this chat, or the chat id is wrong: %w", err)
	case tg.IsChannelInvalid(err):
		return fmt.Errorf("telegram did not resolve the channel: either the Bot API -> MTProto "+
			"id conversion is wrong, or access_hash=0 was rejected: %w", err)
	default:
		return err
	}
}

// ErrAdminRequired means the bot is in the group but not an administrator.
//
// This is the EXPECTED first outcome, not an exception: a bot is very often added as a plain
// member and promoted minutes later. The caller must treat it as "not yet" and wait for the
// promotion event — never as a permanent failure.
var ErrAdminRequired = errors.New("the bot is not an administrator of this group yet")

// ErrLegacyBasicGroup means the chat is a pre-supergroup "basic" group, which has no
// channels.getParticipants at all.
//
// This is PERMANENT and not the bot's to fix: only a human can upgrade the chat (and
// Telegram does it automatically on various triggers). Callers must treat it as an expected
// dead end and say so at WARN — never as an unknown failure at ERROR, which is what it used
// to be when this was an unwrapped fmt.Errorf string that errors.Is could not match.
var ErrLegacyBasicGroup = errors.New("this is a legacy basic group, not a supergroup: " +
	"channels.getParticipants does not apply")

// toChannelID converts a Bot API chat id to a bare MTProto channel id. See botAPIChannelBase.
func toChannelID(botAPIID int64) (int64, error) {
	switch {
	case botAPIID > 0:
		// Already a bare MTProto id, or a private chat, which has no members to list.
		return botAPIID, nil
	case botAPIID > -botAPIChannelBase:
		// A legacy BASIC group. These do not go through channels.getParticipants at all —
		// their member list arrives via messages.getFullChat. Fail loudly rather than emit a
		// nonsense id that would surface as a misleading CHANNEL_INVALID.
		return 0, fmt.Errorf("chat %d: %w", botAPIID, ErrLegacyBasicGroup)
	default:
		id := -botAPIID - botAPIChannelBase
		if id <= 0 {
			return 0, fmt.Errorf("chat %d did not convert to a positive channel id", botAPIID)
		}
		return id, nil
	}
}

// TrimmedTokenHint helps diagnose the commonest misconfiguration without logging the token.
func TrimmedTokenHint() string {
	tok := os.Getenv("BOT_TOKEN")
	if tok == "" {
		return "BOT_TOKEN is empty"
	}
	if strings.TrimSpace(tok) != tok {
		return "BOT_TOKEN has surrounding whitespace"
	}
	return ""
}
