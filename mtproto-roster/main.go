// Command mtproto-roster answers ONE question: can this bot enumerate the full member
// list of a supergroup?
//
// # Why this exists
//
// The Bot API (which mensa-bot-refactor speaks via go-telegram/bot) cannot list group
// members. There is no such method: getChatMember needs a user ID you already have,
// getChatAdministrators returns only admins, getChatMemberCount returns only an integer.
// That matters because group_associations is only ever written when a join request is
// approved, so members who were already in the groups before the bot existed are invisible
// to it — cleanup cannot kick them and /elimina cannot remove them.
//
// MTProto — the protocol the official clients and TDLib speak — DOES have the method:
// channels.getParticipants. The docs say "Both users and bots can use this method", gated
// on CHAT_ADMIN_REQUIRED. If that holds for our bot, a one-shot enumeration at cutover can
// seed group_associations for the whole existing membership and the problem disappears.
//
// This program exists to prove or disprove that BEFORE anyone designs around it. Telegram
// has historically tightened member enumeration for anti-spam reasons, so the documented
// behavior is a claim to be tested, not a fact to be assumed.
//
// # The access_hash problem (and why access_hash=0 is correct here)
//
// channels.getParticipants takes an InputChannel{ChannelID, AccessHash}. A bot never
// learns an access_hash — the Bot API does not expose one. This looks like a dead end,
// and it is the most likely way for a naive attempt to fail (with CHANNEL_INVALID) for
// the wrong reason, leading to a false "enumeration is blocked" conclusion.
//
// It is not a dead end. https://core.telegram.org/api/peers specifies:
//
//	"Zero access hash: equal to 0, must be used by bots when only a min access hash
//	 (or no access hash) is available locally, but a full access hash is required."
//
// So AccessHash: 0 is not a placeholder or a hack — it is the documented, mandated value
// for a bot in exactly this position. Telegram resolves the channel server-side.
//
// # Usage
//
//	TG_API_ID=...  TG_API_HASH=...  BOT_TOKEN=...  TARGET_CHAT_ID=-1001234567890 \
//	    go run .
//
// TG_API_ID / TG_API_HASH come from https://my.telegram.org (an interactive login on a
// personal Telegram account; they are NOT derivable from the bot token). TARGET_CHAT_ID is
// in the Bot API's format — the negative -100… number you already see in the bot's logs.
//
// Point it at a THROWAWAY test supergroup first. It is read-only, but a real group means
// pulling real members' IDs and names onto your terminal for no reason.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

// pageLimit is the participants-per-request page size. Telegram caps this server-side at
// 200; asking for more is silently truncated, so the pagination loop must key off what
// actually came back rather than assuming a full page.
const pageLimit = 200

// pageDelay throttles pagination. Enumeration is the exact access pattern Telegram's
// anti-spam rate limiter watches, and tripping a FLOOD_WAIT during the one measurement we
// care about would be indistinguishable from the feature being blocked.
const pageDelay = 300 * time.Millisecond

// hardPageCap stops a runaway loop if the offset/emptiness contract ever surprises us.
// 200 pages * 200 per page = 40k members, far above any Mensa group.
const hardPageCap = 200

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "\nFAILED: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	apiID, apiHash, botToken, chatID, err := loadConfig()
	if err != nil {
		return err
	}

	channelID, err := toMTProtoChannelID(chatID)
	if err != nil {
		return err
	}

	fmt.Printf("target chat id : %d (Bot API format)\n", chatID)
	fmt.Printf("mtproto channel: %d\n", channelID)
	fmt.Println()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	client := telegram.NewClient(apiID, apiHash, telegram.Options{})

	return client.Run(ctx, func(ctx context.Context) error {
		// Bot auth over MTProto. This is importBotAuthorization under the hood, which is
		// why it needs api_id/api_hash on top of the bot token.
		if _, err := client.Auth().Bot(ctx, botToken); err != nil {
			return fmt.Errorf("bot authentication failed: %w", err)
		}
		fmt.Println("[ok] authenticated as a bot over MTProto")

		api := client.API()

		channel, err := resolveChannel(ctx, api, channelID)
		if err != nil {
			return err
		}

		if err := reportSelfRights(ctx, api, channel); err != nil {
			// Not fatal: the enumeration below is the actual measurement. But knowing
			// whether we are admin is what distinguishes "blocked for bots" from
			// "blocked because we lack rights".
			fmt.Printf("[warn] could not read own membership: %v\n", err)
		}

		return enumerate(ctx, api, channel)
	})
}

// resolveChannel turns a bare channel id into an InputChannel carrying the REAL access_hash.
//
// This two-step is not optional, and the reason is subtle enough that it cost a debugging
// round to find. https://core.telegram.org/api/peers says a bot "must use" access_hash=0
// when it has none — and that IS true, but only for RESOLUTION. Passing 0 straight to
// channels.getParticipants returns CHANNEL_INVALID; passing it to channels.getChannels
// resolves fine AND hands back the channel's real access_hash, which the participants call
// then accepts.
//
// So the zero hash is a key that opens exactly one door: the one that gives you the real key.
//
// This matters beyond tidiness: CHANNEL_INVALID from getParticipants looks identical to
// "Telegram forbids bots from enumerating members", which is the false negative that would
// have killed the cutover plan. It does not. It just means we skipped this step.
func resolveChannel(ctx context.Context, api *tg.Client, channelID int64) (*tg.InputChannel, error) {
	res, err := api.ChannelsGetChannels(ctx, []tg.InputChannelClass{
		&tg.InputChannel{ChannelID: channelID, AccessHash: 0},
	})
	if err != nil {
		return nil, fmt.Errorf("resolving channel %d with the zero access hash: %w", channelID, interpret(err))
	}

	chats, ok := res.(*tg.MessagesChats)
	if !ok {
		return nil, fmt.Errorf("resolving channel %d: unexpected response type %T", channelID, res)
	}

	for _, c := range chats.Chats {
		ch, ok := c.(*tg.Channel)
		if !ok || ch.ID != channelID {
			continue
		}
		fmt.Printf("[ok] resolved channel %d (%q), got its real access_hash\n", ch.ID, ch.Title)
		return &tg.InputChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}, nil
	}

	return nil, fmt.Errorf("channel %d did not resolve: the bot is probably not a member of it, "+
		"or TARGET_CHAT_ID is wrong", channelID)
}

// enumerate pages through channels.getParticipants and reports what came back.
func enumerate(ctx context.Context, api *tg.Client, channel *tg.InputChannel) error {
	fmt.Println("\n--- channels.getParticipants ---")

	seen := make(map[int64]struct{})
	reportedTotal := -1
	offset := 0

	for page := 0; ; page++ {
		if page >= hardPageCap {
			return fmt.Errorf("hit the %d-page cap without the list ending; pagination contract is not what we assumed", hardPageCap)
		}

		res, err := api.ChannelsGetParticipants(ctx, &tg.ChannelsGetParticipantsRequest{
			Channel: channel,
			// Recent is the plain "everyone" filter. It is also the one with the
			// well-known ~10k practical ceiling; if a real group ever exceeds that we
			// would need ChannelParticipantsSearch over letter prefixes instead.
			Filter: &tg.ChannelParticipantsRecent{},
			Offset: offset,
			Limit:  pageLimit,
			Hash:   0,
		})
		if err != nil {
			return interpret(err)
		}

		participants, ok := res.(*tg.ChannelsChannelParticipants)
		if !ok {
			// ChannelsChannelParticipantsNotModified only happens with a non-zero Hash,
			// which we never send.
			return fmt.Errorf("unexpected response type %T", res)
		}

		if reportedTotal < 0 {
			reportedTotal = participants.Count
			fmt.Printf("server reports Count = %d\n", reportedTotal)
		}

		got := len(participants.Participants)
		if got == 0 {
			break
		}

		for _, u := range participants.Users {
			user, ok := u.(*tg.User)
			if !ok {
				continue
			}
			seen[user.ID] = struct{}{}
		}

		fmt.Printf("  page %-3d offset=%-6d got=%-4d unique users so far=%d\n", page, offset, got, len(seen))
		offset += got

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pageDelay):
		}
	}

	fmt.Println("\n--- VERDICT ---")
	fmt.Printf("unique users enumerated: %d\n", len(seen))
	fmt.Printf("server-reported Count  : %d\n", reportedTotal)

	switch {
	case len(seen) == 0:
		fmt.Println("ENUMERATION RETURNED NOTHING. The call succeeded but yielded no members —")
		fmt.Println("treat this as NOT PROVEN. Check the bot really is an admin of this chat.")
	case reportedTotal >= 0 && len(seen) < reportedTotal:
		fmt.Printf("PARTIAL: got %d of %d. The member list may be capped or partly hidden.\n", len(seen), reportedTotal)
		fmt.Println("The cutover tool would under-count, so do NOT plan a kick policy on this yet.")
	default:
		fmt.Println("ENUMERATION WORKS. A one-shot cutover tool can snapshot the full membership,")
		fmt.Println("seed group_associations, and the grace-period kick can reach existing members.")
	}

	return nil
}

// reportSelfRights prints what the bot's own membership in the chat looks like. The whole
// experiment is gated on CHAT_ADMIN_REQUIRED, so "are we actually admin here" is the first
// thing to rule out when enumeration fails.
func reportSelfRights(ctx context.Context, api *tg.Client, channel *tg.InputChannel) error {
	res, err := api.ChannelsGetParticipant(ctx, &tg.ChannelsGetParticipantRequest{
		Channel:     channel,
		Participant: &tg.InputPeerSelf{},
	})
	if err != nil {
		return interpret(err)
	}

	switch p := res.Participant.(type) {
	case *tg.ChannelParticipantAdmin:
		fmt.Println("[ok] this bot IS an administrator of the target chat")
		fmt.Printf("     rights: %+v\n", p.AdminRights)
	case *tg.ChannelParticipantCreator:
		fmt.Println("[ok] this bot is the CREATOR of the target chat")
	default:
		fmt.Printf("[warn] this bot is a PLAIN MEMBER (%T), not an admin.\n", p)
		fmt.Println("       channels.getParticipants lists CHAT_ADMIN_REQUIRED — expect it to fail.")
	}
	return nil
}

// interpret turns the RPC errors that actually matter into a plain statement of what they
// mean for the cutover plan. A raw "rpc error code 400: CHANNEL_INVALID" is exactly the
// kind of thing that gets misread as "Telegram blocks this".
func interpret(err error) error {
	switch {
	case tg.IsChatAdminRequired(err):
		return fmt.Errorf("CHAT_ADMIN_REQUIRED: the bot is not an admin of this chat. "+
			"This does NOT disprove enumeration — promote the bot and retry: %w", err)
	case tg.IsChannelPrivate(err):
		return fmt.Errorf("CHANNEL_PRIVATE: the bot is not in this chat at all, or the chat id is wrong. "+
			"Check TARGET_CHAT_ID against the bot's own logs: %w", err)
	case tg.IsChannelInvalid(err):
		return fmt.Errorf("CHANNEL_INVALID: Telegram did not resolve the channel id. Most likely the "+
			"Bot API -> MTProto id conversion is wrong, or access_hash=0 was rejected for this bot "+
			"(which WOULD be a real finding, contradicting core.telegram.org/api/peers): %w", err)
	default:
		return err
	}
}

func loadConfig() (apiID int, apiHash, botToken string, chatID int64, err error) {
	rawID := os.Getenv("TG_API_ID")
	apiHash = os.Getenv("TG_API_HASH")
	botToken = os.Getenv("BOT_TOKEN")
	rawChat := os.Getenv("TARGET_CHAT_ID")

	var missing []string
	if rawID == "" {
		missing = append(missing, "TG_API_ID")
	}
	if apiHash == "" {
		missing = append(missing, "TG_API_HASH")
	}
	if botToken == "" {
		missing = append(missing, "BOT_TOKEN")
	}
	if rawChat == "" {
		missing = append(missing, "TARGET_CHAT_ID")
	}
	if len(missing) > 0 {
		return 0, "", "", 0, fmt.Errorf("missing required env: %v\n\n"+
			"TG_API_ID and TG_API_HASH come from https://my.telegram.org (interactive login on a\n"+
			"personal account). They are required IN ADDITION to BOT_TOKEN: MTProto bot auth is\n"+
			"importBotAuthorization, which takes all three", missing)
	}

	apiID, err = strconv.Atoi(rawID)
	if err != nil {
		return 0, "", "", 0, fmt.Errorf("TG_API_ID must be an integer: %w", err)
	}

	chatID, err = strconv.ParseInt(rawChat, 10, 64)
	if err != nil {
		return 0, "", "", 0, fmt.Errorf("TARGET_CHAT_ID must be an integer: %w", err)
	}

	return apiID, apiHash, botToken, chatID, nil
}

// toMTProtoChannelID converts a Bot API chat id to the bare MTProto channel id.
//
// The two APIs number the same chat differently: the Bot API prefixes supergroups and
// channels with -100, so MTProto channel 1234567890 appears as -1001234567890 in every log
// line the bot writes. Feeding the Bot API form straight into InputChannel yields
// CHANNEL_INVALID, which reads exactly like "Telegram blocked us" — hence the explicit
// conversion and the guard rails below.
func toMTProtoChannelID(botAPIID int64) (int64, error) {
	const botAPIChannelBase = 1_000_000_000_000

	switch {
	case botAPIID > 0:
		// Already a bare MTProto id (or a user/private chat id, which has no members to list).
		return botAPIID, nil
	case botAPIID > -botAPIChannelBase:
		// e.g. -123456789: a legacy BASIC group, not a supergroup. Basic groups do not go
		// through channels.getParticipants at all — their full member list arrives via
		// messages.getFullChat instead. Fail loudly rather than emit a nonsense id.
		return 0, fmt.Errorf("chat id %d looks like a legacy BASIC group, not a supergroup. "+
			"channels.getParticipants does not apply; basic groups expose members via "+
			"messages.getFullChat. Point this at a supergroup", botAPIID)
	default:
		id := -botAPIID - botAPIChannelBase
		if id <= 0 {
			return 0, errors.New("chat id did not convert to a positive channel id")
		}
		return id, nil
	}
}
