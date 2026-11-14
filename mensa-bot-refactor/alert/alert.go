// Package alert sends operational alerts to the bot's admins over Telegram.
//
// Until it existed, everything an operator had to act on (a tripped circuit breaker, a
// failed sweep, a member the bot gave up removing) went only to the logs, where nobody sees
// it without grepping. Recipients are ADMIN_TELEGRAM_IDS. An admin only receives alerts
// after they have pressed Start on the bot at least once: a bot cannot open a private chat.
//
// Alerts are in English and do not go through Tolgee: they are for operators, not members.
package alert

import (
	"context"

	"github.com/Bombaclath97/mensa-telegram-bot/database"
	"github.com/Bombaclath97/mensa-telegram-bot/logger"
	tb "github.com/Bombaclath97/mensa-telegram-bot/telegram_bot"
)

// recipients is a variable so tests can supply their own admins.
var recipients = database.SeededBotAdmins

// Admins sends text (ParseMode HTML: escape interpolated values with tolgee.EscapeHTML) to
// every configured admin. Best effort: a failed send is logged and never returned, so an
// alert can never break the code that raised it. Returns how many admins received it.
func Admins(ctx context.Context, text string) int {
	ids := recipients()
	if len(ids) == 0 {
		logger.Warn("alert: no admins configured (ADMIN_TELEGRAM_IDS), alert not delivered", map[string]interface{}{"text": text})
		return 0
	}
	sent := 0
	for _, id := range ids {
		if _, err := tb.Send(ctx, id, text); err != nil {
			logger.Warn("alert: could not deliver to admin (have they pressed Start on the bot?)", map[string]interface{}{
				"admin_id": id,
				"error":    err.Error(),
			})
			continue
		}
		sent++
	}
	return sent
}
