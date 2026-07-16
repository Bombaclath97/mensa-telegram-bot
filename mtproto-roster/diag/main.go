// Command diag isolates WHY channels.getParticipants returns CHANNEL_INVALID.
//
// Three candidate causes, which mean completely different things for the cutover plan:
//
//  1. the Bot API -> MTProto id conversion is wrong        -> our bug, fixable
//  2. access_hash=0 is rejected for channels                -> contradicts core.telegram.org/api/peers
//  3. the bot must "learn" the peer before addressing it    -> fixable, needs a resolution step
//
// It tries each in turn and reports which one is true. Read-only throughout.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

func main() {
	apiID, _ := strconv.Atoi(os.Getenv("TG_API_ID"))
	apiHash := os.Getenv("TG_API_HASH")
	token := os.Getenv("BOT_TOKEN")
	botAPIID, _ := strconv.ParseInt(os.Getenv("TARGET_CHAT_ID"), 10, 64)

	converted := -botAPIID - 1_000_000_000_000

	client := telegram.NewClient(apiID, apiHash, telegram.Options{})
	ctx := context.Background()

	if err := client.Run(ctx, func(ctx context.Context) error {
		if _, err := client.Auth().Bot(ctx, token); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
		fmt.Println("[ok] authed as bot")
		api := client.API()

		// --- Test 1: getChannels with the converted id + access_hash 0 -------------
		// If access_hash=0 works at all for channels, this is the cheapest call that
		// proves it.
		fmt.Printf("\n[test 1] channels.getChannels id=%d hash=0\n", converted)
		probe(ctx, api, converted, 0)

		// --- Test 2: the raw Bot API id, in case the conversion is the problem -----
		fmt.Printf("\n[test 2] channels.getChannels id=%d (raw bot-api id) hash=0\n", botAPIID)
		probe(ctx, api, botAPIID, 0)

		// --- Test 2b: what does channelFull say about hidden participants? ---------
		// participants_hidden is Telegram's "Hide Members" group setting. The docs define
		// it only as "Whether the participant list is hidden" and say nothing about
		// whether an ADMIN still sees the list — which is exactly what the cutover plan
		// hangs on. Read the live flag so we at least know the test group's baseline.
		fmt.Println("\n[test 2b] channels.getFullChannel — participants_hidden?")
		if full, err := api.ChannelsGetFullChannel(ctx, &tg.InputChannel{
			ChannelID: converted, AccessHash: realHash(ctx, api, converted),
		}); err != nil {
			fmt.Printf("  getFullChannel failed: %v\n", err)
		} else if cf, ok := full.FullChat.(*tg.ChannelFull); ok {
			fmt.Printf("  participants_hidden = %v\n", cf.ParticipantsHidden)
			fmt.Printf("  participants_count  = %d\n", cf.ParticipantsCount)
			fmt.Printf("  can_view_participants = %v\n", cf.CanViewParticipants)
		}

		// --- Test 3: can the bot learn the peer from its own update stream? --------
		// A fresh MTProto session knows no peers. If Telegram hands us the channel WITH
		// a real access_hash here, then cause (3) is the answer and the fix is to seed
		// the peer from updates rather than to invent a hash.
		fmt.Println("\n[test 3] updates.getState + getDifference — does the channel arrive with a real access_hash?")
		state, err := api.UpdatesGetState(ctx)
		if err != nil {
			fmt.Printf("  getState failed: %v\n", err)
			return nil
		}
		fmt.Printf("  state: pts=%d qts=%d date=%d\n", state.Pts, state.Qts, state.Date)

		diff, err := api.UpdatesGetDifference(ctx, &tg.UpdatesGetDifferenceRequest{
			Pts:  1,
			Qts:  0,
			Date: 1,
		})
		if err != nil {
			fmt.Printf("  getDifference failed: %v\n", err)
			return nil
		}
		reportChats(diff)
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}
}

// realHash resolves the channel's true access_hash via the zero-hash door. See
// resolveChannel in ../main.go for why this two-step is mandatory.
func realHash(ctx context.Context, api *tg.Client, id int64) int64 {
	res, err := api.ChannelsGetChannels(ctx, []tg.InputChannelClass{
		&tg.InputChannel{ChannelID: id, AccessHash: 0},
	})
	if err != nil {
		return 0
	}
	chats, ok := res.(*tg.MessagesChats)
	if !ok {
		return 0
	}
	for _, c := range chats.Chats {
		if ch, ok := c.(*tg.Channel); ok && ch.ID == id {
			return ch.AccessHash
		}
	}
	return 0
}

func probe(ctx context.Context, api *tg.Client, id, hash int64) {
	res, err := api.ChannelsGetChannels(ctx, []tg.InputChannelClass{
		&tg.InputChannel{ChannelID: id, AccessHash: hash},
	})
	if err != nil {
		fmt.Printf("  -> FAILED: %v\n", err)
		return
	}
	chats, ok := res.(*tg.MessagesChats)
	if !ok {
		fmt.Printf("  -> unexpected type %T\n", res)
		return
	}
	for _, c := range chats.Chats {
		if ch, ok := c.(*tg.Channel); ok {
			fmt.Printf("  -> RESOLVED: id=%d access_hash=%d title=%q\n", ch.ID, ch.AccessHash, ch.Title)
		}
	}
	if len(chats.Chats) == 0 {
		fmt.Println("  -> returned zero chats")
	}
}

func reportChats(diff tg.UpdatesDifferenceClass) {
	var chats []tg.ChatClass
	switch d := diff.(type) {
	case *tg.UpdatesDifference:
		chats = d.Chats
	case *tg.UpdatesDifferenceSlice:
		chats = d.Chats
	case *tg.UpdatesDifferenceEmpty:
		fmt.Println("  difference is EMPTY — a fresh bot session has no peer history")
		return
	default:
		fmt.Printf("  difference type %T\n", d)
		return
	}
	if len(chats) == 0 {
		fmt.Println("  no chats in difference")
		return
	}
	for _, c := range chats {
		if ch, ok := c.(*tg.Channel); ok {
			fmt.Printf("  CHANNEL: id=%d access_hash=%d title=%q\n", ch.ID, ch.AccessHash, ch.Title)
		}
	}
}
