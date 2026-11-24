# Mensa Bot Telegram

Un bot Telegram che fa da portinaio ai gruppi Telegram di Mensa Italia. Un socio
dimostra una sola volta di essere iscritto; da quel momento il bot approva in
automatico le sue richieste di ingresso in tutti i gruppi che amministra, ne
conserva il profilo e rimuove periodicamente chi non è più socio.

Il bot parla italiano (predefinito), inglese, spagnolo e tedesco. I testi rivolti
agli utenti arrivano da [Tolgee](https://tolgee.io), con CDN e cache su disco come
ripiego, così un disservizio delle traduzioni non manda mai giù il bot.

Vuoi contribuire? Leggi [`CONTRIBUTING.md`](CONTRIBUTING.md).

## Architettura

È un **unico servizio Go** (modulo `github.com/Mensa-Italia/mensa-telegram-bot`,
un solo binario). Sostituisce il vecchio deployment in tre parti (`mensa-bot` +
`mensa-telegram-crud` + `mensa-shared-models`), ormai dismesso.

Il processo esegue due cose in parallelo, sotto un contesto condiviso che viene
annullato dai segnali (SIGINT/SIGTERM → spegnimento ordinato):

- **Long-polling di Telegram**: comandi, callback, controllo delle richieste di
  ingresso e eventi di appartenenza ai gruppi.
- **Un server HTTP in ingresso** (`HTTP_ADDR`, predefinito `:8080`) che espone:
  - `GET /health`: liveness probe (usata dall'healthcheck di Docker/compose).
  - `POST /callme_api/{token}`: la callback che l'app ufficiale Mensa chiama per
    approvare o rifiutare una richiesta di verifica dell'identità.

La persistenza è **PostgreSQL, usato direttamente tramite GORM**
(`gorm.io/driver/postgres`, Go puro). Non c'è un servizio CRUD separato né uno
strato HTTP/DB tra il bot e i suoi dati. Lo schema viene migrato in automatico
all'avvio e le foreign key sono applicate dal database.

Il bot è **stateless**: non tiene nulla su disco, e di proposito. Prima usava
SQLite, che legava il processo al filesystem di una sola macchina e rendeva Docker
Swarm pericoloso (un volume con nome è locale al suo nodo e non segue un task
rischedulato altrove, quindi il bot sarebbe ripartito con un database vuoto).
Postgres elimina anche la dipendenza da cgo: per questo l'immagine ora è un binario
statico distroless da 27 MB invece di una base Debian con dietro un toolchain C.

Attenzione: questo **non** rende il bot scalabile. `replicas` deve essere
esattamente 1. Telegram consente un solo poller `getUpdates` per token, e sia
l'archivio delle bozze sia il timer della pulizia vivono nel processo. Vedi
[`DEPLOY.md`](DEPLOY.md).

Il servizio **non** gestisce il TLS: serve HTTP in chiaro e va messo dietro un
reverse proxy (nginx, Caddy, Traefik, …) che termina il TLS e inoltra a
`HTTP_ADDR` sia il percorso della webhook sia l'health check.

```
                 ┌────────────────────────────────────┐
   Telegram  ◄──►│  mensa-bot (unico binario Go)      │
  (long poll)    │    • handler bot   ─┐              │       ┌────────────┐
                 │    • server http   ─┼─► GORM ──────┼──────►│  Postgres  │
 reverse proxy   │    • timer pulizia ─┘              │       │  (volume)  │
  ──HTTP──►:8080 │                                    │       └────────────┘
 (/callme_api,   │    uscita ─► API Mensa, Tolgee     │      l'unico stato,
  /health)       └────────────────────────────────────┘      legato a un nodo
                          stateless: nessun volume
```

## Registrazione e verifica

L'iscrizione si dimostra con un giro attraverso l'app ufficiale Mensa: il bot non
si fida mai, da sola, di un'email o di un numero di tessera dichiarati dall'utente.

1. L'utente apre una chat privata e lancia `/profilo` (o `/start`) per avviare la
   procedura guidata di registrazione.
2. La procedura chiede l'**email @mensa.it** e il **numero di tessera**.
3. All'invio il bot chiama l'API Mensa (`POST {API_ENDPOINT}/request`) con email,
   numero di tessera e un URL di callback monouso (`{CALLME_BASE_URL}/{token}`).
   Una risposta 200 significa che la coppia corrisponde a un socio reale e che è
   partita una notifica push verso la sua app Mensa. La bozza viene bloccata nello
   stato `AWAITING_APPROVAL` (scadenza `APPROVAL_TIMEOUT_MINUTES`).
4. Il socio apre l'app Mensa e approva (o rifiuta). L'app richiama
   `POST /callme_api/{token}` con `{"accepted": <bool>}`. Il token deve
   corrispondere alla bozza bloccata in **entrambi** i casi, così nessun terzo può
   approvare o annullare la registrazione di qualcun altro.
5. Dopo l'approvazione il bot chiede nome e cognome reali, scrive la riga
   dell'utente verificato e da quel momento l'account è un socio riconosciuto.
6. Da lì in poi, ogni **richiesta di ingresso** a un gruppo amministrato dal bot
   viene approvata in automatico per quel socio, e l'associazione al gruppo viene
   registrata. Se a chiedere l'ingresso è un utente non registrato, il bot gli
   scrive in privato di registrarsi prima e mette la richiesta in attesa; **la
   registrazione stessa svuota la coda**, quindi quelle richieste vengono
   approvate appena l'utente finisce. `/approva` rilancia lo svuotamento a mano,
   se mai servisse.
7. La registrazione controlla anche tutti i gruppi noti e registra quelli in cui
   il socio era *già* presente. Nient'altro potrebbe scoprirli: chi era nel gruppo
   prima dell'arrivo del bot non invia mai una richiesta di ingresso, e la Bot API
   non permette di elencare i membri di un gruppo. Senza questo passaggio quei soci
   sarebbero invisibili a `/elimina`, all'elenco gruppi di `/profilo` e alla
   pulizia periodica, che cancellerebbe la loro riga senza espellerli da nulla.

I soci di Mensa internazionale senza un indirizzo `@mensa.it` vengono aggiunti da
un admin del bot con il comando nascosto `/manual_add`, che salta il controllo via
email e app.

Una **pulizia periodica** guidata da un timer (intervallo `CLEANUP_INTERVAL_HOURS`)
verifica ogni socio contro `POST {API_ENDPOINT}/valid`. I soci scaduti vengono
avvisati, declassati o espulsi dai gruppi e cancellati. Di serie la pulizia gira
in **dry-run** (`CLEANUP_DRY_RUN=true`) e ha dei circuit breaker
(`CLEANUP_MAX_DELETIONS`, `CLEANUP_MAX_FRACTION`), così una configurazione sbagliata
non può rimuovere in massa i soci.

## Comandi

Pubblicati nel menu comandi di Telegram (per lingua) tramite `SetMyCommands`:

| Comando    | Cosa fa                                                             |
| ---------- | ------------------------------------------------------------------- |
| `/start`   | Saluta e ti dice se sei registrato.                                 |
| `/profilo` | Mostra profilo e gruppi, oppure avvia la registrazione.             |
| `/approva` | Approva le richieste di ingresso in attesa, una volta registrato.   |
| `/app`     | Invia i link per scaricare l'app Mensa Italia.                      |
| `/elimina` | Cancella il tuo profilo e ti rimuove da tutti i gruppi Mensa.       |
| `/lang`    | Scegli la lingua (it / en / es / de).                               |
| `/cancel`  | Interrompe una procedura guidata in corso.                          |

Altri quattro comandi sono **solo per admin** e di proposito non compaiono nel menu.
Chi non è admin non riceve alcuna risposta. Gli admin vengono inizializzati da
`ADMIN_TELEGRAM_IDS`.

| Comando          | Cosa fa                                                                  |
| ---------------- | ------------------------------------------------------------------------ |
| `/manual_add`    | Registra un socio internazionale senza indirizzo `@mensa.it`.            |
| `/sweep_preview` | Esegue subito una pulizia in dry-run e riporta chi verrebbe rimosso.     |
| `/pending`       | Elenca le rimozioni dai gruppi che `/elimina` non ha ancora completato.  |
| `/groups`        | Elenca tutti i gruppi, se il bot vi è admin e i rispettivi membri.       |

Gli stessi admin ricevono avvisi su Telegram quando serve l'intervento di una
persona: un circuit breaker scattato, una pulizia fallita, l'API Mensa non in
salute, una rimozione a cui il bot ha rinunciato, oppure il bot rimosso da un
gruppo o privato dei diritti di admin. Vedi
[`DEPLOY.md`](DEPLOY.md#admin-alerts).

## Configurazione

Tutta la configurazione passa da variabili d'ambiente. Vedi
[`mensa-bot-refactor/ENVIRONMENT.md`](mensa-bot-refactor/ENVIRONMENT.md) per
l'elenco completo e [`mensa-bot-refactor/.env.example`](mensa-bot-refactor/.env.example)
per un modello da copiare. L'essenziale:

| Variabile            | Obbligatoria | Descrizione                                            |
| -------------------- | ------------ | ------------------------------------------------------ |
| `BOT_TOKEN`          | sì           | Token del bot Telegram da [@BotFather](https://t.me/BotFather). |
| `API_ENDPOINT`       | sì           | URL base dell'API soci di Mensa Italia.                |
| `API_BEARER_TOKEN`   | sì           | Bearer token per quell'API.                            |
| `CALLME_BASE_URL`    | sì           | Origine pubblica di questo servizio + `/callme_api`.   |
| `ADMIN_TELEGRAM_IDS` | no           | ID Telegram separati da virgola, segnati come admin del bot. |
| `DATABASE_URL`       | sì*          | DSN Postgres. *Oppure la forma separata `DB_HOST`/`DB_USER`/`DB_PASSWORD`/`DB_NAME`. |
| `HTTP_ADDR`          | no           | Indirizzo di ascolto (predefinito `:8080`).            |

## Avvio

### Docker Compose (consigliato)

1. Copia il modello delle variabili d'ambiente e compila i valori obbligatori:

   ```sh
   cp mensa-bot-refactor/.env.example mensa-bot-refactor/.env
   $EDITOR mensa-bot-refactor/.env
   ```

2. Compila e avvia il servizio:

   ```sh
   docker compose up -d --build
   ```

I dati di Postgres stanno nel volume con nome `mensa-pgdata`, quindi sopravvivono
alla sostituzione del container. Il bot non ha volumi: è stateless. Il servizio
`mensa-bot` pubblica la porta `8080`: puntaci il reverse proxy e inoltra
`/callme_api/…`.

Per **Docker Swarm / Portainer** usa [`docker-stack.yml`](docker-stack.yml) e leggi
prima [`DEPLOY.md`](DEPLOY.md): `docker stack deploy` ignora `build:`, quindi serve
un'immagine pubblicata, e i dati legano lo stack a un nodo etichettato.

### In locale (sviluppo)

Servono Go 1.26.5+ e un Postgres raggiungibile. Non serve più un toolchain C: la
build è `CGO_ENABLED=0` e completamente statica.

```sh
docker compose up -d postgres        # oppure punta DATABASE_URL al tuo
cd mensa-bot-refactor
cp .env.example .env                 # poi compilalo
go run .
```

La suite di test avvia un suo Postgres usa e getta tramite testcontainers, quindi
richiede un demone Docker attivo; senza, i test vengono saltati invece di fallire.

```sh
go test ./...
```

## Traduzioni

Le chiavi di traduzione sono documentate in
[`mensa-bot-refactor/TOLGEE_KEYS.md`](mensa-bot-refactor/TOLGEE_KEYS.md), che
indica le chiavi nuove di questo refactor e che devono esistere nel progetto Tolgee
(in it / en / es / de): per qualsiasi chiave mancante il bot mostra la chiave grezza
con i punti.

## Licenza

Rilasciato con licenza [GNU Affero General Public License v3.0](LICENSE). Se
distribuisci una versione modificata, o la fai girare come servizio accessibile in
rete, devi rendere disponibile il relativo codice sorgente con la stessa licenza.
