# Come contribuire

Grazie per l'interesse! Ogni contributo è benvenuto: bug report, idee, traduzioni,
documentazione e codice.

## Regole del repository

- Il branch `main` è protetto: **nessuno fa push diretti**. Ogni modifica entra
  tramite pull request.
- Chi non è maintainer contribuisce **da una fork**: fai la fork del repository,
  lavora su un branch della tua fork e apri una PR verso `main`.
- Una PR viene unita solo con la **CI verde** e **l'approvazione di un maintainer**.
- Per i contributori esterni la CI parte solo dopo l'approvazione di un maintainer.
  Se la tua PR resta in attesa, non è un errore.

## Flusso di lavoro

1. Fai la fork su GitHub e clonala:

   ```sh
   git clone git@github.com:<tuo-utente>/mensa-telegram-bot.git
   cd mensa-telegram-bot
   git remote add upstream https://github.com/Bombaclath97/mensa-telegram-bot.git
   ```

2. Parti sempre da un `main` aggiornato e crea un branch con un nome parlante:

   ```sh
   git fetch upstream
   git switch -c fix/descrizione-breve upstream/main
   ```

   Prefissi consigliati: `feat/`, `fix/`, `docs/`, `test/`, `refactor/`, `chore/`.

3. Fai le modifiche, esegui i controlli (vedi sotto) e fai commit.
4. Fai push sulla tua fork e apri una PR verso `Bombaclath97/mensa-telegram-bot:main`.
5. Rispondi alle review: i commit aggiuntivi sullo stesso branch aggiornano la PR.

Per modifiche grosse o che cambiano il comportamento del bot verso i soci, **apri
prima una issue** per discuterne: eviti di lavorare su qualcosa che poi non viene
accettato.

## Ambiente di sviluppo

Requisiti: Go 1.26.5+, Docker (per Postgres e per i test).

```sh
docker compose up -d postgres
cd mensa-bot-refactor
cp .env.example .env      # compila i valori; per lo sviluppo puoi usare mock-mensa-api
go run .
```

`compose.dev.yaml` e `mock-mensa-api/` permettono di lavorare senza accesso all'API
reale di Mensa Italia. Vedi il [README](README.md) per i dettagli.

## Prima di aprire la PR

La CI esegue questi controlli; lanciali in locale da `mensa-bot-refactor/`:

```sh
go mod tidy && git diff --exit-code go.mod go.sum   # go.mod in ordine
gofmt -l .                                          # deve stampare niente
go vet ./...
go test -race ./...                                 # richiede Docker attivo
```

Se tocchi `mtproto-roster/` o `mock-mensa-api/`, esegui `go build ./...` e
`go test ./...` anche lì.

## Stile

- **Commit** in stile [Conventional Commits](https://www.conventionalcommits.org/):
  `feat: ...`, `fix: ...`, `docs: ...`, `test: ...`, `refactor: ...`, `chore: ...`.
  Il messaggio spiega *perché*, non solo *cosa*.
- Codice formattato con `gofmt`. I commenti spiegano le decisioni non ovvie.
- Nuovi comportamenti vanno coperti da test, specialmente quelli che rimuovono soci
  dai gruppi o cancellano dati.
- I testi mostrati agli utenti passano da Tolgee: non scrivere stringhe a mano nel
  codice. Se aggiungi una chiave, documentala in
  [`mensa-bot-refactor/TOLGEE_KEYS.md`](mensa-bot-refactor/TOLGEE_KEYS.md).

## Dati personali e segreti

Il bot gestisce dati personali dei soci. **Non committare mai**:

- file `.env`, token, chiavi API o password;
- database, dump o log con dati reali (nomi, email, ID Telegram, numeri di tessera).

Nei test e negli esempi usa solo dati inventati.

## Segnalare una vulnerabilità

Non aprire una issue pubblica per problemi di sicurezza. Usa
[Private vulnerability reporting](https://github.com/Bombaclath97/mensa-telegram-bot/security/advisories/new)
di GitHub.

## Licenza

Contribuendo accetti che il tuo contributo sia rilasciato con la licenza del
progetto, [AGPL-3.0](LICENSE). Se il tuo contributo viene unito, aggiungiti pure a
[`CONTRIBUTORS.md`](CONTRIBUTORS.md) nella stessa PR.
