# Mensa Bot Telegram

Mensa Bot Telegram è un bot per Telegram che permette agli utenti di registrarsi e gestire il proprio profilo per essere approvati in tutti i gruppi di Mensa.

## Funzionalità

- Registrazione degli utenti
- Gestione del profilo utente (nome, cognome, email)
- Invito agli utenti non registrati a creare un profilo
- Memorizzazione temporanea delle informazioni degli utenti durante la registrazione

## Installazione

1. Clona il repository:

    ```sh
    git clone https://git.bombaclath.cc/bombaclath97/mensa-bot-telegram.git
    cd mensa-bot-telegram
    ```

2. Crea un file [.env](http://_vscodecontentref_/0) nella directory [mensa-bot](http://_vscodecontentref_/1) e aggiungi il token del :

bot    ```dotenv
    TOKEN=il_tuo_token_del_bot
    ```

3. Installa le dipendenze:

    ```sh
    go mod tidy
    ```

4. Avvia il bot:

    ```sh
    go run mensa-bot/main.go
    ```

## Utilizzo

### Comandi disponibili

- `/start` - Avvia il bot e verifica se l'utente è registrato.
- `/profile` - Mostra il profilo dell'utente se è registrato, altrimenti avvia la procedura di registrazione.

### Registrazione

Se l'utente non è registrato, il bot lo inviterà a creare un profilo utilizzando il comando `/start`. Durante la registrazione, il bot chiederà all'utente di fornire il proprio nome, cognome e email.

## Struttura del progetto

- [main.go](http://_vscodecontentref_/2) - Il file principale che avvia il bot.
- [handlers.go](http://_vscodecontentref_/3) - Contiene i gestori per i vari comandi del bot.
- [statefulObjects.go](http://_vscodecontentref_/4) - Contiene le strutture dati per memorizzare temporaneamente le informazioni degli utenti durante la registrazione.
- `mensa-bot/constants/const.go` - Contiene le costanti utilizzate nel progetto.

## Contribuire

Se desideri contribuire al progetto, sentiti libero di aprire una pull request o segnalare un problema.

## Licenza

Questo progetto è distribuito sotto la licenza MIT. Vedi il file LICENSE per maggiori dettagli.