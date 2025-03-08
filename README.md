# Mensa Bot Telegram

Mensa Bot Telegram è un bot per Telegram che permette agli utenti di registrarsi e gestire il proprio profilo per essere approvati in tutti i gruppi di Mensa.

## Installazione

1. Clona il repository:

   ```sh
   git clone https://git.bombaclath.cc/bombaclath97/mensa-bot-telegram.git
   cd mensa-bot-telegram
   ```

2. Crea un file `env` nella directory `mensa-bot` e valorizza le seguenti variabili d'ambiente:
   | Variabile        | Valore                                                                  |
   | ---------------- | ----------------------------------------------------------------------- |
   | TOKEN            | Il token generato da [@BotFather](https://t.me/BotFather)               |
   | API_ENDPOINT     | L'endpoint da utilizzare per comunicare con la app di Mensa Italia      |
   | API_BEARER_TOKEN | Il bearer token da utilizzare per comunicare con la app di Mensa Italia |
   | GMAIL_USERNAME   | La mail google da utilizzare per mandare il codice di verifica          |
   | GMAIL_PASSWORD   | La password da utilizzare per autenticarsi a google                     |

3. Installa le dipendenze:

   ```sh
   go mod tidy
   ```

4. Avvia il bot:

   ```sh
   go run mensa-bot/main.go
   ```
