package model

const (
	// INVITE_TO_JOIN_MESSAGE is the message sent to users who are not registered
	INVITE_TO_JOIN_MESSAGE                = "Ciao %s! Sono il bot di Mensa. Per favore, crea un profilo usando il comando `/start` per essere approvato in tutti i gruppi."
	NOT_REGISTERED_MESSAGE                = "Non sei registrato. Per favore, crea un profilo usando il comando `/profile`."
	ALREADY_REGISTERED_MESSAGE            = "Ciao %s! Sei già registrato. Puoi visualizzare il tuo profilo usando il comando `/profile`."
	INITIATE_PROFILE_REGISTRATION_MESSAGE = "Creiamo il tuo profilo. Ti chiederò in ordine nome, cognome ed email. **Per favore inseriscili come appaiono in Area32**.\nQual è il tuo nome?"
	ASK_SURNAME_MESSAGE                   = "Ottimo, %s. Me ne ricorderò!\nQual è il tuo cognome invece?"
	ASK_EMAIL_MESSAGE                     = "%s %s. Grazie! Qual è il tuo indirizzo email? Inserisci quella con dominio `@mensa.it`, per favore."
	ASK_CONFIRMATION_CODE_MESSAGE         = "Ho mandato un codice di conferma all'indirizzo email %s. Per favore, inseriscilo qui."
	INVALID_EMAIL_MESSAGE                 = "L'indirizzo email inserito non è valido. Per favore, inserisci un indirizzo email con dominio `@mensa.it`."
	PROFILE_SHOW_MESSAGE                  = `Il profilo legato a questo account:\n
	\n
	| Nome | Cognome | Email |\n
	| ---- | ------- | ----- |\n
	| %s | %s | %s |\n
	\n`

	EMAIL_BODY = `<h1 id="ciao-s-ecco-il-tuo-codice">Ciao %s, ecco il tuo codice</h1>
					<p><code>%s</code></p>
					<p>Non condividere questo codice con nessuno. Se hai ricevuto questa mail ma non sai a cosa è riferita, per favore ignorala e segnala questo abuso su telegram a <a href="https://t.me/Bombaclath97">@Bombaclath97</a></p>
					<p><em>Floreat Mensa</em></p>
					`
)
