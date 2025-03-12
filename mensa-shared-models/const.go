package model

const (
	// Automatic messages sent by bot to new users
	INVITE_TO_JOIN_MESSAGE = "Ciao %s! Sono il bot di Mensa. Per favore, crea un profilo usando il comando `/profilo` per essere approvato in tutti i gruppi."

	// Sent with command /start
	NOT_REGISTERED_MESSAGE     = "Non sei registrato. Per favore, crea un profilo usando il comando `/profilo`."
	ALREADY_REGISTERED_MESSAGE = "Ciao %s! Sei già registrato. Puoi visualizzare il tuo profilo usando il comando `/profilo`."

	// Sent with command /profilo
	// Ordine: NUMERO DI TESSERA -> CERCA SE ESISTE -> CHIEDI NOME E COGNOME -> MANDA MAIL CON CODICE -> SE OK TUTTO SALVA SALVA
	PROFILE_SHOW_MESSAGE = `Il profilo legato a questo account:
	
	- Nome: %s
	- Cognome: %s
	- Email: %s`
	INITIATE_PROFILE_REGISTRATION_MESSAGE = `Creiamo il tuo profilo. Puoi annullare il processo in ogni momento usando il comando /cancel. 
	Prima di cominciare, per favore assicurati di:
	- Avere scaricato l'app ufficiale MENSA Italia (usa il comando /app per scaricarla) e di aver già fatto l'accesso almeno una volta
	- Avere a portata di mano il tuo numero di tessera

	Ti chiederò in ordine la tua mail ed il tuo numero di tessera, poi nome e cognome. Se in qualsiasi momento dovessi avere problemi, ti prego di contattare @Bombaclath97.
	
	Qual è la tua email?`
	EMAIL_NOT_VALID_MESSAGE            = "L'indirizzo email inserito non è valido. Per favore, inserisci un indirizzo email con dominio `@mensa.it`."
	EMAIL_ALREADY_REGISTERED           = "L'indirizzo email inserito è già registrato a un altro account telegram. Se pensi che ci sia un errore, contatta @Bombaclath97."
	ASK_MEMBER_NUMBER_MESSAGE          = "Grazie! Ora, per favore, inserisci il tuo numero di tessera."
	MEMBER_NUMBER_IS_NOT_VALID_MESSAGE = "Perdonami, il messaggio che hai inserito non è un numero di tessera. Per favore, inserisci un numero di tessera valido."
	NON_EXISTENT_ASSOCIATION_MESSAGE   = "Mi dispiace, ho cercato un utente con indirizzo email %s e numero di tessera %d, ma non ho trovato nessuna associazione. Per favore, controlla i dati inseriti e riprova. Puoi cominciare da capo usando il comando `/profilo`."
	AWAIT_APPROVAL_ON_APP              = "Ho mandato una richiesta di approvazione all'app Mensa Italia. Per favore, controlla l'app e approva la richiesta."
	ASK_NAME_MESSAGE                   = "La tua mail ed il tuo numero di tessera sono validi! Qual è il tuo nome?"
	ASK_SURNAME_MESSAGE                = "Ottimo, %s. Me ne ricorderò!\nQual è il tuo cognome invece?"
	REGISTRATION_SUCCESS_MESSAGE       = "Il codice è corretto! Il tuo profilo è stato creato con successo. Utilizza il comando `/approve` per essere approvato nei gruppi in cui hai fatto richiesta."

	// Sent with command /approve
	NO_REQUESTS_TO_APPROVE    = "Non ci sono richieste da approvare al momento. Potrebbe essere un malfunzionamento del bot. Prova a richiedere l'accesso al gruppo di nuovo, altrimenti contatta @Bombaclath97."
	REQUESTS_APPROVED_MESSAGE = "Richiesta approvata con successo! Ora puoi scrivere nel gruppo."

	// Sent with command /cancel
	CANCEL_REGISTRATION_MESSAGE = "Registrazione annullata. Per favore, usa il comando `/profilo` per ricominciare."

	// Sent with command /app
	APP_DOWNLOAD_MESSAGE = `
	Per utilizzare il bot è necessario aver scaricato l'app ufficiale Mensa Italia. Puoi scaricarla da qui:

	- [App Store](https://play.google.com/store/apps/details?id=it.mensa.app&hl=it&pli=1)
	- [Google Play](https://play.google.com/store/apps/details?id=it.mensa.app)
	
	Se non possiedi uno smartphone android o iOS, contatta @Bombaclath97 per assistenza.
	`

	// Email body sent with confirmation code to new users
	EMAIL_BODY = `<h1 id="ciao-s-ecco-il-tuo-codice">Ciao %s, ecco il tuo codice</h1>
	<p><code>%s</code></p>
	<p>Non condividere questo codice con nessuno. Se hai ricevuto questa mail ma non sai a cosa è riferita, per favore ignorala e segnala questo abuso su telegram a <a href="https://t.me/Bombaclath97">@Bombaclath97</a></p>
	<p><em>Floreat Mensa</em></p>
	`
)
