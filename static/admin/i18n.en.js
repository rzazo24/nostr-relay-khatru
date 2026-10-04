// Diccionario español → inglés del panel (ver i18n.js). Generado a partir de los textos del panel; si cambias un texto en español,
// su traducción aquí deja de encontrarse (el hash cambia) y la prueba de inglés lo señala: actualiza la entrada.
//  · html: textos fijos de index.html, por hash FNV-1a del original en español (el comentario guarda el comienzo).
//  · ui:   frases que escribe admin.js / nip46.js con t('…'), por el propio texto en español; {nombre} son huecos.
window.PANEL_EN = {
  html: {
    "5c679508": "<b>Language</b>: the <b>EN</b> / <b>ES</b> buttons at the top switch the panel between English and Spanish, help included. It is remembered in that browser; the first time the browser's language decides.", /* Idioma: los botones EN / ES de arriba cambian el panel entre inglés y español... */
    "1fc93583": "Each event type has its own <b>colour badge</b> (the number and the name) and the card border in the same colour: <b>notes</b> in teal, <b>reactions</b> in pink, <b>profiles and lists</b> in blue, <b>private messages</b> in violet, <b>app data</b> in lime green, <b>ephemeral</b> in light blue, <b>authentication</b> in orange, <b>deletions and reports</b> in red and <b>zaps</b> in yellow. The same badges appear in “Rejections” and in “Noisiest keys”.", /* Cada tipo de evento lleva su insignia de color… */
    "4b13e002": "Relay panel", /* Panel del relé */
    "179a5a16": "Loading…", /* Cargando… */
    "530cc80d": "Help", /* Ayuda */
    "7cc1e2b3": "Refresh", /* Actualizar */
    "c387cd17": "Log out", /* Cerrar sesión */
    "135d2dab": "Log in", /* Entrar */
    "6b27f165": "This panel is only for the relay owner. You log in by signing with your Nostr key (NIP-07 extension, for example nos2x): the private key never leaves the extension.", /* Este panel es solo para el dueño del relé. Entras firmando c */
    "be1f2335": "Log in with Nostr", /* Entrar con Nostr */
    "4929e9a0": "Log in from your phone with a remote signer (Clave on iPhone…)", /* Entrar desde el móvil con un firmador remoto (Clave en el iP */
    "01f1c5e0": "No browser extension needed: the key stays in the signer app (for example <b>Clave</b>, which uses the NIP-46 standard) and the panel only receives the signature. The exchange goes through this relay and Clave's relay (<code>relay.powr.build</code>), end-to-end encrypted.", /* Sin extensión en el navegador: la clave se queda en la app f */
    "12b9a9cd": "Connect with a remote signer", /* Conectar con un firmador remoto */
    "85ac5875": "Tap <b>Open Clave</b> (if it doesn't open, use <b>Copy link</b> and paste it into Clave, under <i>Connect</i>) and <b>approve</b> the connection.", /* Pulsa <b>Abrir Clave</b> (si no se abre, usa <b>Copiar enlac */
    "b2a72728": "Come back to this page: it will continue on its own. You will also have to approve the login signature.", /* Vuelve a esta página: seguirá sola. Tendrás que aprobar tamb */
    "b70f59b4": "Open Clave", /* Abrir Clave */
    "30fb0ee1": "Copy link", /* Copiar enlace */
    "ea3d2b54": "Cancel", /* Cancelar */
    "fc660dec": "Summary", /* Resumen */
    "d7cd00b3": "What does each number mean?", /* ¿Qué significa cada número? */
    "b20bda76": "·", /* · */
    "e48de9bc": "Download a full copy of the database right now", /* Descarga ahora una copia completa de la base de datos */
    "f43d3063": "Download backup now", /* Descargar copia de seguridad ahora */
    "9880eb80": "Activity", /* Actividad */
    "f6531db3": "What is this", /* Qué es esto */
    "52a57ce3": "Help about this section", /* Ayuda sobre esta sección */
    "3a0cb08e": "?", /* ? */
    "097c4211": "Period", /* Periodo */
    "5431687b": "60 min", /* 60 min */
    "bebabd13": "24 h", /* 24 h */
    "6a4cd915": "7 days", /* 7 días */
    "110bcc95": "30 days", /* 30 días */
    "bdb2cbc3": "90 days", /* 90 días */
    "699e1ad6": "Activity for the chosen period", /* Actividad del periodo elegido */
    "9b517038": "<i class=\"sw saved\"></i> stored", /* <i class="sw saved"></i> guardados */
    "e0d2f46a": "<i class=\"sw ephemeral\"></i> ephemeral", /* <i class="sw ephemeral"></i> efímeros */
    "f3755df5": "<i class=\"sw rejected\"></i> rejected", /* <i class="sw rejected"></i> rechazados */
    "1b0a13b1": "Growth · events stored per day", /* Crecimiento · eventos guardados por día */
    "7232dc4e": "Events by kind", /* Eventos por tipo */
    "493b3d06": "Rejections", /* Rechazos */
    "3728bdfd": "Time", /* Hora */
    "c46b03e6": "What", /* Qué */
    "d9d80e9f": "Kind", /* Tipo */
    "5b065950": "Key", /* Clave */
    "8a089ae5": "Reason", /* Motivo */
    "f9801514": "Noisiest keys", /* Claves más ruidosas */
    "d0165aef": "The keys whose events the relay has rejected the most in the last 24 h (since the last start). Tap a key to copy it in full.", /* Las claves a las que el relé ha rechazado más eventos en las */
    "0f19fcac": "Last", /* Último */
    "d94117e3": "Recent events", /* Eventos recientes */
    "b0eef65c": "Content of public notes, shortened. Private messages don't show their content. Tap a key to copy it in full.", /* Contenido de las notas públicas, recortado. Los mensajes pri */
    "2123cf5d": "Search", /* Buscar */
    "ff4a941a": "What to search for", /* Qué buscar */
    "8471e200": "npub…, key or id (whole or its first characters), kind number or text", /* npub…, clave o id (entero o sus primeros caracteres), número */
    "4136f5ea": "Filter by event kind", /* Filtrar por tipo de evento */
    "89a5e51f": "kind (optional)", /* tipo (opcional) */
    "54c53860": "From", /* Desde */
    "23eccaba": "From date", /* Desde la fecha */
    "e12f665c": "To", /* Hasta */
    "4e8f0776": "To date", /* Hasta la fecha */
    "943acb3d": "Quick ranges", /* Rangos rápidos */
    "564e2e23": "Today", /* Hoy */
    "e100623d": "Clear", /* Limpiar */
    "66319562": "Moderation", /* Moderación */
    "57a07605": "Changes apply immediately and are saved (they survive restarts). As the owner, you are never blocked by these lists.", /* Los cambios se aplican al momento y se guardan (sobreviven a */
    "7d08ba16": "Banned keys", /* Claves baneadas */
    "df248352": "Key to ban", /* Clave a banear */
    "058c305a": "npub… or key in hex", /* npub… o clave en hex */
    "b2942481": "Reason (optional)", /* Motivo (opcional) */
    "ca7fc630": "also delete their stored events", /* borrar también sus eventos guardados */
    "1398d54e": "Ban", /* Banear */
    "515a950b": "Author allow-list", /* Lista blanca de autores */
    "19800909": "Empty = the relay is open. With at least one key, only those (and you) can publish; reading stays free.", /* Vacía = el relé es abierto. Con al menos una clave, solo esa */
    "41753579": "Key to allow", /* Clave a permitir */
    "1040bb11": "Note", /* Nota */
    "31c94df5": "Note (optional)", /* Nota (opcional) */
    "d4f2636d": "Allow", /* Permitir */
    "5a0516e5": "Vetoed events", /* Eventos vetados */
    "705fdfc7": "Event to veto", /* Evento a vetar */
    "4e042379": "event id (hex, note1… or nevent1…)", /* id del evento (hex, note1… o nevent1…) */
    "7accb044": "Veto and delete", /* Vetar y borrar */
    "93ef4670": "Event kinds", /* Tipos de evento */
    "2c84a770": "“Forbid” rejects that kind. If you mark any as “allow”, only the allowed ones get through (deletion, kind 5, always gets through).", /* «Prohibido» rechaza ese tipo. Si marcas alguno como «permiti */
    "31044a7f": "Event kind", /* Tipo de evento */
    "d913e243": "kind", /* kind */
    "add2090e": "Rule", /* Regla */
    "44d52e0c": "forbid", /* prohibir */
    "59836c4d": "allow", /* permitir */
    "9d5e19e3": "Apply", /* Aplicar */
    "6e666fb2": "Blocked IPs", /* IPs bloqueadas */
    "9996a7ac": "The relay doesn't log IPs: this is for blocking an address you already know.", /* El relé no registra IPs: esto sirve para bloquear una direcc */
    "c4740a96": "IP to block", /* IP a bloquear */
    "ada2e526": "IP address", /* dirección IP */
    "f802c63e": "Block", /* Bloquear */
    "c1f5a2f7": "Relay information", /* Información del relé */
    "7de9fd0a": "What the relay announces (and the landing page shows). For two languages write the description as <code>English | Español</code>.", /* Lo que anuncia el relé (y muestra la página de presentación) */
    "1e684c40": "Name", /* Nombre */
    "ff85be51": "changed", /* cambiado */
    "700b72de": "Description", /* Descripción */
    "f985b4df": "changed", /* cambiada */
    "2fa2d32f": "Icon (https://… or /icon.png)", /* Icono (https://… o /icon.png) */
    "b401e7be": "empty = no icon", /* vacío = sin icono */
    "c4ae0ac4": "Contact", /* Contacto */
    "cce54751": "email, NIP-05, npub… (empty = none)", /* correo, NIP-05, npub… (vacío = ninguno) */
    "85215054": "Tags", /* Etiquetas */
    "797b6ac4": "changed", /* cambiadas */
    "3c588d36": "general, open (comma-separated)", /* general, open (separadas por comas) */
    "230dc39f": "Languages", /* Idiomas */
    "618a4f86": "changed", /* cambiados */
    "95f851d3": "en, es (language codes, comma-separated)", /* en, es (códigos de idioma, separados por comas) */
    "e2baaee1": "Posting policy (https://…)", /* Normas de uso (https://…) */
    "4cafc986": "empty = no policy", /* vacío = sin normas */
    "291705dd": "Save", /* Guardar */
    "9c219c33": "Restore the configuration values", /* Restaurar los de la configuración */
    "82ebf33a": "Action history", /* Historial de acciones */
    "b75ab918": "What has been done on the relay as owner: bans, vetoes, rules, information changes and logins. The latest 100 (1,000 are kept).", /* Lo que se ha hecho en el relé como dueño: baneos, vetos, reg */
    "486ef5ab": "When", /* Cuándo */
    "2808e140": "Action", /* Acción */
    "72aeaf7d": "Target", /* Objetivo */
    "e09447ee": "Configuration", /* Configuración */
    "d2dfadc6": "Panel help", /* Ayuda del panel */
    "7ac34a83": "Close the help", /* Cerrar la ayuda */
    "e8438e2a": "Close", /* Cerrar */
    "bbef8d79": "Index", /* Índice */
    "c5e23303": "Numbers at the top", /* Números de arriba */
    "6bde1ddf": "Growth", /* Crecimiento */
    "c32516b7": "Header and session", /* Cabecera y sesión */
    "b002bc63": "Glossary", /* Glosario */
    "c1744b01": "The numbers at the top", /* Los números de arriba */
    "510da68f": "Events stored", /* Eventos guardados */
    "53d432e7": "How many events the relay has stored right now. Ephemeral events (see “Activity”) don't count, because they aren't stored.", /* Cuántos eventos tiene el relé almacenados ahora mismo. Los e */
    "79887354": "Distinct keys", /* Claves distintas */
    "a3463e11": "How many different public keys have published something that is still stored. A rough way to know how many different people use the relay.", /* Cuántas claves públicas diferentes han publicado algo que si */
    "b703a863": "Last 24 h", /* Últimas 24 h */
    "5116481b": "How many of the stored events are dated within the last 24 hours.", /* Cuántos de los eventos guardados tienen fecha de las últimas */
    "8eb3f0cc": "Database", /* Base de datos */
    "befe1725": "How much disk space the file where events are stored takes (including its temporary write log).", /* Lo que ocupa en disco el archivo donde se guardan los evento */
    "94ab3c87": "Open connections", /* Conexiones abiertas */
    "1da1e7fe": "Clients connected right now. One app can open several connections, so this isn't exactly the number of people.", /* Clientes conectados en este momento. Una misma app puede abr */
    "42b56aa3": "Disk used", /* Disco usado */
    "2235b2f8": "How much of the server's disk is used (the disk where the database lives) and how much is free. It turns red from 85 %: you should make room before it fills up.", /* Qué parte del disco del servidor está ocupada (la del disco  */
    "ce66266f": "Last backup", /* Última copia de seguridad */
    "1ee5132c": "When the most recent copy of the database was made, how big it is and how many are stored. It only appears if the relay knows where the backups are (the <code>RELAY_BACKUP_DIR</code> variable). It turns red if the latest one is more than 36 hours old or there is none: the automatic backup runs every night.", /* Cuándo se hizo la copia más reciente de la base de datos, cu */
    "f2edb894": "The link under the numbers downloads <b>right now</b> a full copy of the database (a <code>.sqlite.gz</code> file), in addition to the one made automatically every night. It is consistent even while the relay is writing, and it is restored the same way as the automatic ones with <code>scripts/restore-db.sh</code>. <b>It contains everything the relay stores</b>, including private (encrypted) messages and your moderation lists, so treat it as a secret. It is built in the browser's memory, so it is meant for databases of up to a few hundred MB. It is recorded in the history.", /* El enlace bajo los números descarga <b>en ese momento</b> un */
    "654409d3": "Rejections since start", /* Rechazos desde el arranque */
    "deefee38": "Publications and queries the relay has rejected since it started. It resets to zero when the relay restarts. A high number isn't bad in itself: it is usually other people's traffic held back by the rate limits (see “Rejections”).", /* Publicaciones y consultas que el relé ha rechazado desde que */
    "5650290b": "The chart shows how much activity the relay has had. Choose the period with the buttons:", /* La gráfica muestra cuánta actividad ha tenido el relé. Elige */
    "04f1dd5a": "<b>60 min</b>: one bar per minute, live. It lives only in memory: if the relay restarts, it starts from zero.", /* <b>60 min</b>: una barra por minuto, en directo. Vive solo e */
    "db07cfc1": "<b>24 h</b> and <b>7 days</b>: one bar per hour.", /* <b>24 h</b> y <b>7 días</b>: una barra por hora. */
    "1e8b6de1": "<b>30 days</b> and <b>90 days</b>: one bar per day.", /* <b>30 días</b> y <b>90 días</b>: una barra por día. */
    "70413290": "Hours and days are shown in <b>your local time</b>. Long periods are stored in the relay's database, only as <b>per-hour counters</b> (no content, keys or IPs), and kept for a year. They survive restarts and updates, and <b>start on the day this feature was enabled</b>: anything earlier shows as zero.", /* Las horas y los días se muestran en <b>tu hora local</b>. Lo */
    "f94937bf": "<b>Colors:</b> <span class=\"kw saved\">teal</span> = stored events, <span class=\"kw ephemeral\">blue</span> = ephemeral events, <span class=\"kw rejected\">red</span> = rejected. Hover over a bar to see its exact figures (including authentications). The bars are stacked: their total height is the sum of the three.", /* <b>Colores:</b> <span class="kw saved">turquesa</span> = eve */
    "cd1fd25e": "Under the chart there is a <b>summary of the chosen period</b>:", /* Debajo de la gráfica hay un <b>resumen del periodo elegido</ */
    "abd6cbbd": "Stored", /* Guardados */
    "266297ca": "Events the relay accepted and stored during the period.", /* Eventos que el relé aceptó y guardó en el periodo. */
    "47d065e1": "Ephemeral", /* Efímeros */
    "7211c75c": "Events the relay forwarded to whoever was listening but <b>did not store</b> (momentary messages, for example live chats). They take no disk space.", /* Eventos que el relé reenvió a quien escuchaba pero <b>no gua */
    "91801bfb": "Rejected", /* Rechazados */
    "c35bbdcf": "Publications and queries the relay rejected (see “Rejections” for the reasons).", /* Publicaciones y consultas que el relé rechazó (mira «Rechazo */
    "0e0f2de8": "Authentications", /* Autenticaciones */
    "f6ec3269": "Times a client identified itself to the relay (NIP-42). It's what apps do to be able to read private messages.", /* Veces que un cliente se identificó ante el relé (NIP-42). Es */
    "51dd57fb": "Max simultaneous connections", /* Conexiones máx. a la vez */
    "4d3f70c5": "The highest number of connections open at once seen in any hour of the period (it doesn't appear for 60 min).", /* El mayor número de conexiones abiertas a la vez que se vio e */
    "4b277109": "Rejections by reason", /* Rechazos por motivo */
    "26659e31": "The period's rejections grouped by reason (not available for 60 min: that's what the “Rejections” block is for).", /* Los rechazos del periodo agrupados por motivo (sin disponibi */
    "943cb5fa": "Size at the start and end of the period and how much it has grown. Useful to predict when the disk would fill up.", /* Tamaño al principio y al final del periodo y cuánto ha creci */
    "2d1270c0": "How many events were stored at the start and end of the period, and the difference. It can go down if <b>retention</b> deletes old events.", /* Cuántos eventos había guardados al principio y al final del  */
    "3687c864": "If a figure says “—” or is missing, it wasn't recorded yet for that period (remember the history starts on the day it was enabled).", /* Si algún dato dice «—» o falta, es que en ese periodo aún no */
    "16c50c98": "How many of the events stored <b>now</b> were created on each day (UTC date) during the last 14 days. It shows how fast the relay is growing.", /* Cuántos de los eventos que hay guardados <b>ahora</b> se cre */
    "2c4a180c": "It isn't the same as “Events stored” in the activity summary: here events are counted by the <b>date they were created with</b>, not by when they reached the relay, and only those still stored count (if retention deletes something, it disappears from this chart). To see the real growth in size use “Database” and “Events stored” in the activity summary.", /* No es lo mismo que «Eventos guardados» del resumen de activi */
    "faed2d9e": "In Nostr every event has a <b>kind</b> number that says what it is. The table counts how many there are of each. The most common ones:", /* En Nostr cada evento tiene un número de <b>tipo</b> (en ingl */
    "e93aa6f0": "0 · Profile", /* 0 · Perfil */
    "caad5ca7": "Name, picture and description of an account.", /* Nombre, foto y descripción de una cuenta. */
    "4afa87a7": "1 · Note", /* 1 · Nota */
    "6512eea9": "A normal public message.", /* Un mensaje público normal. */
    "1367a3a9": "3 · Contacts", /* 3 · Contactos */
    "fe3e9ed1": "Who an account follows.", /* A quién sigue una cuenta. */
    "fb9eb055": "4 · Direct message (legacy)", /* 4 · Mensaje directo (antiguo) */
    "82fa363b": "Encrypted private message in the old format. The relay only delivers it to its author and its recipient.", /* Mensaje privado cifrado del formato antiguo. El relé solo lo */
    "4fb3532a": "5 · Deletion", /* 5 · Borrado */
    "af2b653c": "An account asks to withdraw one of its events.", /* Una cuenta pide retirar un evento suyo. */
    "c58d63ed": "7 · Reaction", /* 7 · Reacción */
    "ce776a66": "A “like” or another emoji on a note.", /* Un «me gusta» u otro emoji sobre una nota. */
    "b218769d": "1059 · Gift wrap", /* 1059 · Gift wrap */
    "c8304dff": "Wrapper for modern private messages. Also private: only its recipient sees it.", /* Envoltorio de los mensajes privados modernos. También privad */
    "f2adaa34": "10002 · Relay list", /* 10002 · Lista de relés */
    "0acdfe72": "The relays an account uses (Damus publishes it when you add a relay).", /* Los relés que usa una cuenta (Damus lo publica al añadir un  */
    "e588293f": "10050 · Direct-message relays", /* 10050 · Relés de mensajes directos */
    "d0df8e1f": "Where an account wants to receive its private messages.", /* Dónde quiere recibir una cuenta sus mensajes privados. */
    "c740f524": "20001 · Ephemeral", /* 20001 · Efímero */
    "4a0c8649": "Momentary messages from some application; they are not stored.", /* Mensajes momentáneos de alguna aplicación; no se guardan. */
    "50b5331d": "If you see a number without a name, it is an uncommon kind; you can look it up in the Nostr documentation (the “NIPs”).", /* Si ves un número sin nombre, es un tipo poco habitual; puede */
    "6469cbb6": "A list of what the relay has said no to, with the reason. The labels at the top are totals by type of reason <b>since the relay last started</b> (they reset on restart); the table shows the latest rejections. For totals by reason over a longer period, see the summary under the activity chart (24 h or more).", /* Lista de lo que el relé ha dicho que no, con el motivo. Las  */
    "a24ccf99": "Time / What", /* Hora / Qué */
    "5be856cb": "When, and whether it was an <b>event</b> (someone wanted to publish) or a <b>query</b> (someone wanted to read).", /* Cuándo, y si fue una <b>evento</b> (alguien quería publicar) */
    "e292c314": "The kind of event affected; “—” if the query didn't ask for a specific kind.", /* El tipo de evento afectado; «—» si la consulta no pedía un t */
    "f050a608": "The first 8 characters of the public key that tried it (empty if it hadn't authenticated yet). The panel never shows IPs.", /* Los 8 primeros caracteres de la clave pública que lo intentó */
    "bd841577": "Most common reasons:", /* Motivos más habituales: */
    "a0445334": "rate-limited", /* rate-limited */
    "8365d808": "Too fast: that connection exceeded the per-IP rate limit. This is normal against apps that send bursts of ephemeral events; the limit is doing its job.", /* Demasiado rápido: esa conexión superó el límite de velocidad */
    "e1ab90af": "auth-required", /* auth-required */
    "b954bcd2": "Authentication is needed (NIP-42). You'll see it mostly when a client asks for private messages before identifying itself: the client authenticates and repeats the request.", /* Hace falta autenticarse (NIP-42). Lo verás sobre todo cuando */
    "cbd1f530": "invalid", /* invalid */
    "65ffa5f6": "The event doesn't meet the limits: content too long, too many tags, date too far in the future, already expired…", /* El evento no cumple los límites: contenido demasiado largo,  */
    "5a5d6eb3": "blocked", /* blocked */
    "55eb9226": "Moderation vetoes it: banned key, vetoed event or forbidden kind.", /* Lo veta la moderación: clave baneada, evento vetado o tipo p */
    "31f0dbec": "restricted", /* restricted */
    "0afa715b": "The allow-list is active and that key isn't on it.", /* La lista blanca está activa y esa clave no está en ella. */
    "58336ad5": "pow", /* pow */
    "56e423f1": "It doesn't reach the required proof of work (only if enabled).", /* No alcanza la prueba de trabajo exigida (solo si está activa */
    "ae143186": "Ranks whose events have been rejected the most, so you can see at a glance who is abusing without reading the log. It counts only <b>events</b> (not queries), per full key, <b>since the relay last started</b> and only the last 24 h; it is kept in memory, so it resets on restart. Only the 1,000 noisiest keys are remembered.", /* Ordena a quién se le han rechazado más eventos, para ver de  */
    "b2502a21": "The full public key of whoever publishes (tap to copy it). It is the only part of the panel where you see third parties' full keys; IPs are never stored or shown.", /* La clave pública completa de quien publica (pulsa para copia */
    "d23fb814": "How many events from that key have been rejected in total.", /* Cuántos eventos de esa clave se han rechazado en total. */
    "b0a2356a": "Kind / Reason", /* Tipo / Motivo */
    "fbe8c1e1": "The kind of the last rejected event and the most repeated reason (for example “rate-limited”, which means going over the rate limit).", /* El tipo del último evento rechazado y el motivo que más se r */
    "97f11037": "When the last rejection happened.", /* Cuándo fue el último rechazo. */
    "7a4bd7bf": "Buttons", /* Botones */
    "358cbef8": "<b>Ban</b> blocks the key (you can undo it under “Banned keys”). <b>Search</b> opens the search with that key, but it only finds what the relay has <b>stored</b>: ephemeral events, which are almost all of those rejected for speed, are not stored. Your own key can't be banned.", /* <b>Banear</b> bloquea la clave (puedes deshacerlo en «Claves */
    "84d3644d": "Careful: going over the rate limit isn't always abuse (a very active client behind a shared IP also causes it), and anyone can create new keys. Banning is mostly useful against someone who keeps insisting with the same one.", /* Ojo: pasarse del límite de velocidad no siempre es abuso (un */
    "171e051d": "The 30 newest stored events, with the content shortened so you can judge them at a glance. It's what you look at to decide whether something needs moderating.", /* Los 30 eventos guardados más nuevos, con el contenido recort */
    "abc54675": "<b>Private messages</b> show no content: the relay doesn't even send it to the panel.", /* Los <b>mensajes privados</b> no muestran contenido: el relé  */
    "fd791a93": "The <b>yours</b> label marks what was published with your owner key.", /* La etiqueta <b>tuyo</b> marca lo publicado con tu clave de d */
    "08992327": "Tap the key (in teal) to <b>copy it in full</b>.", /* Pulsa la clave (en turquesa) para <b>copiarla entera</b>. */
    "e6c66689": "<b>Veto event</b> deletes that event and doesn't let it be published again. <b>Ban key</b> stops that key from publishing; the page asks whether you also want to delete everything of theirs. Both can be undone from “Moderation”.", /* <b>Vetar evento</b> borra ese evento y no deja que vuelva a  */
    "15ca1234": "Use it to find events or to see what a key has done. Type whatever you have and the panel works out what it is:", /* Sirve para encontrar eventos o ver qué ha hecho una clave. E */
    "cb0b1e5a": "An <code>npub…</code> or a 64-character key", /* Un <code>npub…</code> o una clave de 64 caracteres */
    "f9d0f874": "The events of that account, plus a <b>key summary</b>: how many events it has stored, since when, of which kinds, its profile name (if it published one) and whether it is banned or on the allow-list.", /* Los eventos de esa cuenta, más un <b>resumen de la clave</b> */
    "fd6fa1f2": "An event identifier (<code>note1…</code>, <code>nevent1…</code> or 64 characters)", /* Un identificador de evento (<code>note1…</code>, <code>neven */
    "15e6fb10": "That specific event.", /* Ese evento concreto. */
    "e3913cd7": "The first characters (at least 6) of a key or an id", /* Los primeros caracteres (al menos 6) de una clave o de un id */
    "c860b9f3": "Everything that starts like that. Useful with the abbreviated 8-character keys you see in “Rejections”: tap one and it searches by itself.", /* Todo lo que empiece así. Útil con las claves abreviadas de 8 */
    "8d9c2d77": "A small number", /* Un número pequeño */
    "e847eac7": "The events of that <b>kind</b> (for example <code>7</code> is reactions). You can also use the “kind” box together with any other search to narrow it down.", /* Los eventos de ese <b>tipo</b> (por ejemplo <code>7</code> s */
    "9d2eaa7a": "Any other text", /* Cualquier otro texto */
    "79c2dadd": "Public events whose content contains it (case-insensitive). <b>Private messages are not searched</b> and never show their content.", /* Los eventos públicos cuyo contenido lo contiene (sin disting */
    "6f38b806": "Nothing (empty)", /* Nada (vacío) */
    "a8199e86": "All events, from newest to oldest.", /* Todos los eventos, del más nuevo al más viejo. */
    "aa50707a": "<b>Dates:</b> “From” and “To” (optional, each on its own) narrow any search to that period, counting whole days in your local time; the “Today”, “7 days” and “30 days” buttons fill in both at once. Without search text they let you see what was published in a specific period. A key's summary isn't narrowed by dates: it counts everything of theirs.", /* <b>Fechas:</b> «Desde» y «Hasta» (opcionales, cada una por s */
    "965daf48": "Results come 50 at a time (“Load more” button) with the total on top (counting stops at 10,000). On each event you have <b>Veto event</b> and <b>Ban key</b>, and tapping a key launches a search for everything of theirs. When you see a key, the summary also lets you ban it, or lift the ban if it already is.", /* Los resultados salen de 50 en 50 (botón «Cargar más») con el */
    "ad3a27f5": "Search only reads: it changes nothing until you press a moderation action.", /* La búsqueda solo lee: no cambia nada hasta que pulses una ac */
    "4f299f51": "Everything applies immediately and is saved (it survives restarts). As the owner, you are <b>never blocked</b> by these lists and you can't ban yourself.", /* Todo se aplica al momento y se guarda (sobrevive a reinicios */
    "32eb1160": "A banned key can't publish. You can paste the key as <code>npub…</code> or in hexadecimal. The “also delete their stored events” box removes what they already published (up to 5,000 events). “Remove” lifts the ban.", /* Una clave baneada no puede publicar. Puedes pegar la clave c */
    "bca88c50": "While it is empty, anyone can publish (open relay). As soon as you add <b>one</b> key, the relay becomes <b>write-restricted</b>: only the keys on the list (and you) can publish. Reading stays free. If you remove the last one, it becomes open again. Use it only if you want a private or community relay; the page warns you first.", /* Mientras está vacía, cualquiera puede publicar (relé abierto */
    "eb17865b": "A specific event, by its identifier (hex, <code>note1…</code> or <code>nevent1…</code>). It is deleted if it was stored and is never accepted again. Useful for illegal or annoying content without banning the whole account.", /* Un evento concreto, por su identificador (hex, <code>note1…< */
    "2fbd0211": "<b>Forbid</b> a kind and it is always rejected (for example, a kind that attracts spam). <b>Allow</b> is the opposite and stricter: as soon as you allow one, ONLY the allowed kinds get through and the rest are rejected (deletion, kind 5, always gets through). The page warns you first.", /* <b>Prohibir</b> un tipo lo rechaza siempre (por ejemplo, un  */
    "7bad9b3d": "Blocks an IP address so it can't connect. The relay doesn't store IPs, so this is for when you already know the address from elsewhere.", /* Bloquea una dirección IP para que no pueda conectarse. El re */
    "cd37acde": "What the relay announces and what clients and the landing page show: name, description, icon, <b>contact</b> (an email, a NIP-05 or an npub; it is public), <b>tags</b> (for example “general, open”, used by relay directories to classify it), <b>languages</b> (codes such as “en, es”) and <b>posting policy</b> (an https address). For the description to appear in two languages, write it as <code>English text | Texto en español</code>. “Restore” goes back to the configuration values (those in the <code>.env</code> file). Fields you change here carry a “changed” mark and take precedence over the <code>.env</code> until you restore. The icon can be an <code>https://…</code> address or a path like <code>/icon.png</code>; empty = no icon.", /* Lo que anuncia el relé y muestran los clientes y la página d */
    "819d6c95": "Each action leaves a line in the relay log (<code>admin action=…</code>), with only the first 8 characters of the key or event.", /* Cada acción deja una línea en el registro del relé (<code>ad */
    "af6dad9c": "A record of what you have done as owner, so you remember why something is blocked. It is stored in the relay's database, so it survives restarts; the last 1,000 are kept and here you see the 100 most recent.", /* Un registro de lo que has hecho como dueño, para acordarte d */
    "af17abb8": "Date and time, in your local time.", /* Fecha y hora, en tu hora local. */
    "d7376584": "What was done: banning or unbanning a key or event, allowing or removing from the allow-list, kind rules, IP blocking, relay-information changes and panel <b>logins and logouts</b> (useful to see whether anyone else has logged in).", /* Qué se hizo: banear o quitar el baneo de una clave o un even */
    "16a968a4": "What it affected: a key or an event (shown abbreviated; hover over it to see the full one), an IP or an event kind.", /* A qué afectó: una clave o un evento (se enseñan abreviados;  */
    "97bb7d7a": "The reason you wrote, or for information changes, which fields were changed or restored (never the new content).", /* El motivo que escribiste, o en los cambios de información, q */
    "33e7d04c": "<b>panel</b> if you did it here, or <b>NIP-86</b> if it was with an admin client. Both are recorded.", /* <b>panel</b> si lo hiciste aquí, o <b>NIP-86</b> si fue con  */
    "203be41e": "It doesn't record who you are (only the owner can do these things) or the actions that failed or were rejected.", /* No registra quién eres (solo el dueño puede hacerlo) ni las  */
    "5fd993e2": "Configuration (read-only)", /* Configuración (solo lectura) */
    "837483cd": "These are the limits the relay is working with now. They aren't changed here: they are edited in the server's configuration file and the relay is restarted.", /* Son los límites con los que trabaja el relé ahora. Aquí no s */
    "f2bab761": "NIPs", /* NIPs */
    "ff498c91": "The Nostr specifications the relay supports: 1 (basic protocol), 9 (deletion), 11 (relay information), 40 (expiration), 42 (authentication), 45 (counting), 70 (protected events), 77 (sync) and 86 (management), plus 13 (proof of work) if enabled.", /* Las especificaciones de Nostr que el relé soporta: 1 (protoc */
    "b8399236": "Retention", /* Retención */
    "342b7c84": "How many days regular events are kept (notes, reactions, private messages, deletions…). After that they are deleted automatically, checked once an hour. <b>Not deleted</b>: profiles, contact and relay lists (they are each account's current state) nor anything published by you as owner. “no limit” = everything is kept forever (the database will keep growing).", /* Cuántos días se conservan los eventos normales (notas, reacc */
    "19dc9940": "Max content", /* Contenido máx. */
    "a9ca951c": "Maximum characters of an event's text.", /* Caracteres máximos del texto de un evento. */
    "4ea6a542": "Max message", /* Mensaje máx. */
    "85e53d67": "Maximum size of a whole message sent by a client (text plus tags and signature).", /* Tamaño máximo de un mensaje completo que envía un cliente (t */
    "35aae26e": "Tags per event", /* Tags por evento */
    "1b45fd96": "How many tags an event can carry.", /* Cuántas etiquetas puede llevar un evento. */
    "add6431b": "Events per query", /* Eventos por consulta */
    "69fb913e": "Maximum results a read request returns.", /* Máximo de resultados que devuelve una petición de lectura. */
    "c55d3bfa": "Max NIP-77 sync", /* Sync NIP-77 máx. */
    "8630c7b1": "Maximum events offered in a sync with another relay or client.", /* Máximo de eventos que se ofrecen en una sincronización con o */
    "9b48482f": "Max future date", /* Fecha futura máx. */
    "33e8b90e": "How far ahead an event's date may be compared with the relay's clock.", /* Cuánto puede adelantarse la fecha de un evento respecto al r */
    "5dd991ac": "Proof of work", /* Prueba de trabajo */
    "57a1c971": "A computing effort required when publishing, as anti-spam. “no” = disabled.", /* Un esfuerzo de cálculo que se exige al publicar, como antisp */
    "9fa907b6": "Required auth", /* Auth obligatoria */
    "8a87462f": "Whether authentication is needed to read and write. “no” = only for private messages.", /* Si hace falta autenticarse para leer y escribir. «no» = solo */
    "fc995762": "Private kinds", /* Tipos privados */
    "272256d8": "Event kinds that only their author and their recipient see (direct messages).", /* Tipos de evento que solo ven su autor y su destinatario (men */
    "96dee209": "Events/min per IP · Queries/min per IP · Connections/min per IP", /* Eventos/min por IP · Consultas/min por IP · Conexiones/min p */
    "a14f9c36": "Rate limits per IP address. The first number is how many are allowed per minute; “burst” is how many fit at once before throttling starts.", /* Límites de velocidad por dirección IP. El primer número son  */
    "8aefc1c5": "Under the title you see the relay's <b>version</b> (the identifier of the code that is running, for example <code>9cd2f05</code>) and <b>how long it has been running</b> since its last start.", /* Bajo el título ves la <b>versión</b> del relé (el identifica */
    "d57719e2": "“Updated at…” shows when the data was last requested. It updates by itself every 15 seconds (while the tab is visible); <b>Refresh</b> updates it immediately.", /* «Actualizado a las…» indica cuándo se pidieron los datos por */
    "75cdef83": "You log in by signing with the owner's key (extension such as nos2x). The private key never leaves the extension.", /* Entras firmando con la clave del dueño (extensión como nos2x */
    "e8124165": "<b>From your phone, without an extension</b>: open “Log in from your phone with a remote signer” and tap <b>Connect</b>. A <code>nostrconnect://</code> link appears: tap <b>Open Clave</b> (or copy it and paste it into the app), <b>approve</b> the connection and come back to the page; then also approve the login signature. It is the <b>NIP-46</b> standard: the key stays in the signer app (Clave on iPhone, for example) and the panel only receives the signature. The exchange goes through this same relay, end-to-end encrypted; while you are in the other app the relay keeps those messages <b>10 minutes</b> in memory so the page can pick them up when you come back. Only the owner's key gets in, just like with the extension.", /* <b>Desde el móvil, sin extensión</b>: abre «Entrar desde el  */
    "d183f466": "The session lasts one hour and is kept in a cookie the page can't read. <b>Log out</b> invalidates it instantly. If it expires, the panel tells you and returns to the login screen.", /* La sesión dura una hora y se guarda en una cookie que la pág */
    "158739b9": "<b>On a phone</b> the sections fold: tap the title (the little arrow on the right shows it) to open or close it. “Activity” starts open and the others closed; the panel remembers what you open in that browser. The “?” on each title still opens the help without folding anything, and if you launch a search from another section, “Search” opens by itself. On a computer everything is always open.", /* <b>En el móvil</b> las secciones se pliegan: toca el título  */
    "3427a4b7": "After each moderation action a <b>notice</b> appears at the top with the result (green) or the error (red).", /* Tras cada acción de moderación aparece un <b>aviso</b> arrib */
    "a76cdabb": "The panel never shows IP addresses or the content of private messages.", /* El panel nunca muestra direcciones IP ni el contenido de los */
    "0363d547": "Relay", /* Relé (relay) */
    "82c489d1": "A server that stores and distributes Nostr events.", /* Servidor que guarda y reparte los eventos de Nostr. */
    "043426f0": "Event", /* Evento */
    "9159a85e": "The unit of everything in Nostr: a note, a profile, a reaction… signed by its author.", /* La unidad de todo en Nostr: una nota, un perfil, una reacció */
    "cea2267b": "Public key · npub · hex", /* Clave pública · npub · hex */
    "ba0a8b11": "An account's identity. <code>npub1…</code> is its readable form; “hex” is the same key as 64 hexadecimal characters. They are equivalent.", /* La identidad de una cuenta. <code>npub1…</code> es su forma  */
    "641fb534": "Kind", /* Tipo (kind) */
    "2e43dcc9": "The number that says what class of event it is.", /* El número que indica qué clase de evento es. */
    "9038da6c": "Tag", /* Tag (etiqueta) */
    "f5af3370": "Extra data on an event: who it mentions, what it replies to, hashtags…", /* Dato extra de un evento: a quién menciona, a qué responde, h */
    "63decbc8": "NIP", /* NIP */
    "cd210ad5": "Each of the numbered Nostr specifications (NIP-1, NIP-42…).", /* Cada una de las especificaciones de Nostr, numeradas (NIP-1, */
    "80597348": "Ephemeral", /* Efímero */
    "c4770c88": "An event that is forwarded but not stored.", /* Evento que se reenvía pero no se guarda. */
    "92385b7d": "Burst", /* Ráfaga */
    "11b386e5": "The amount allowed at once before the rate limit starts to act.", /* Cantidad que se permite de golpe antes de que empiece a actu */
  },
  ui: {
    "hace {n} s": "{n} s ago",
    "hace {n} min": "{n} min ago",
    "hace {n} h": "{n} h ago",
    "hace {n} d": "{n} d ago",
    "Preparando la conexión…": "Preparing the connection…",
    "El firmador pide abrir esta dirección para continuar: {u}": "The signer asks you to open this address to continue: {u}",
    "No se pudo preparar la conexión: {e}": "Could not prepare the connection: {e}",
    "Esperando a que apruebes la conexión en la app firmadora…": "Waiting for you to approve the connection in the signer app…",
    "Conectado. Aprueba ahora la firma del inicio de sesión en la app…": "Connected. Now approve the login signature in the app…",
    "el firmador devolvió algo que no es la firma pedida": "the signer returned something that isn't the requested signature",
    "No se pudo entrar: {e}": "Could not log in: {e}",
    "No se detecta ninguna extensión de Nostr. Instala nos2x (o similar) y recarga la página.": "No Nostr extension detected. Install nos2x (or similar) and reload the page.",
    "Firma la petición en la extensión…": "Sign the request in the extension…",
    "Actualizado a las {h}": "Updated at {h}",
    "Error al actualizar: {e}": "Error while updating: {e}",
    "Versión {v} · en marcha desde hace {d}": "Version {v} · running for {d}",
    "Eventos guardados": "Events stored",
    "Claves distintas": "Distinct keys",
    "Últimas 24 h": "Last 24 h",
    "Base de datos": "Database",
    "Conexiones abiertas": "Open connections",
    "Rechazos desde el arranque": "Rejections since start",
    "Disco usado": "Disk used",
    "{b} libres": "{b} free",
    "Última copia de seguridad": "Last backup",
    "ninguna": "none",
    "{b} · {n} guardadas": "{b} · {n} stored",
    "no se encontró ninguna": "none found",
    "Sin eventos en los últimos 14 días.": "No events in the last 14 days.",
    "Todavía no hay eventos.": "There are no events yet.",
    "Ningún rechazo desde el arranque.": "No rejections since the start.",
    "evento": "event",
    "consulta": "query",
    "Ningún evento rechazado en las últimas 24 h.": "No events rejected in the last 24 h.",
    "ninguno": "none",
    "Retención": "Retention",
    "{n} días": "{n} days",
    "sin límite": "no limit",
    "Contenido máx.": "Max content",
    "{n} caracteres": "{n} characters",
    "Mensaje máx.": "Max message",
    "Tags por evento": "Tags per event",
    "Eventos por consulta": "Events per query",
    "Sync NIP-77 máx.": "Max NIP-77 sync",
    "Fecha futura máx.": "Max future date",
    "Prueba de trabajo": "Proof of work",
    "no": "no",
    "Auth obligatoria": "Required auth",
    "sí": "yes",
    "Tipos privados": "Private kinds",
    "Eventos/min por IP": "Events/min per IP",
    "{n} (ráfaga {b})": "{n} (burst {b})",
    "Consultas/min por IP": "Queries/min per IP",
    "Conexiones/min por IP": "Connections/min per IP",
    "Copiar clave completa": "Copy full key",
    "copiada ✓": "copied ✓",
    "Buscar": "Search",
    "Buscar lo que el relé tiene guardado de esta clave (los eventos efímeros no se guardan)": "Search what the relay has stored for this key (ephemeral events are not stored)",
    "tuya": "yours",
    "Banear": "Ban",
    "Banear esta clave": "Ban this key",
    "¿Banear la clave {k}…?\n{n} rechazos ({r}). Dejará de poder publicar; puedes deshacerlo desde «Claves baneadas».": "Ban key {k}…?\n{n} rejections ({r}). It will no longer be able to publish; you can undo it from “Banned keys”.",
    "desde claves más ruidosas": "from noisiest keys",
    "Clave baneada": "Key banned",
    "Buscar todo lo de esta clave": "Search everything from this key",
    "Sus eventos": "Their events",
    "Buscar todo lo que ha publicado esta clave": "Search everything this key has published",
    "tuyo": "yours",
    "Banear clave": "Ban key",
    "Banear la clave de este evento": "Ban the key of this event",
    "¿Banear la clave {k}…?\nDejará de poder publicar. Puedes deshacerlo desde «Claves baneadas».": "Ban key {k}…?\nIt will no longer be able to publish. You can undo it from “Banned keys”.",
    "¿Borrar también todos sus eventos guardados?\n(Aceptar = sí, Cancelar = no, solo banear)": "Also delete all their stored events?\n(OK = yes, Cancel = no, just ban)",
    "desde {s}": "from {s}",
    "Clave baneada y {n} evento(s) borrados": "Key banned and {n} event(s) deleted",
    "Vetar evento": "Veto event",
    "Vetar y borrar este evento": "Veto and delete this event",
    "¿Vetar y borrar este evento? No podrá volver a publicarse.": "Veto and delete this event? It won't be able to be published again.",
    "Evento vetado y borrado": "Event vetoed and deleted",
    "ahora": "now",
    "Guardados": "Stored",
    "Efímeros": "Ephemeral",
    "Rechazados": "Rejected",
    "Autenticaciones": "Authentications",
    "Cargando…": "Loading…",
    "Conexiones máx. a la vez": "Max simultaneous connections",
    "Rechazos por motivo": "Rejections by reason",
    "{l} · guardados {s}, efímeros {e}, rechazados {r}, autenticaciones {a}": "{l} · stored {s}, ephemeral {e}, rejected {r}, authentications {a}",
    "La sesión ha caducado: vuelve a entrar": "The session has expired: log in again",
    "Ninguno": "None",
    "Quitar": "Remove",
    "Baneo quitado": "Ban removed",
    "Es la última clave de la lista blanca: al quitarla el relé vuelve a ser abierto para todos. ¿Continuar?": "This is the last key on the allow-list: removing it makes the relay open to everyone again. Continue?",
    "Clave quitada de la lista blanca": "Key removed from the allow-list",
    "Veto quitado": "Veto removed",
    "Desbloquear": "Unblock",
    "IP desbloqueada": "IP unblocked",
    "prohibido": "forbidden",
    "permitido": "allowed",
    "Regla quitada": "Rule removed",
    "panel": "panel",
    "Todavía no hay acciones anotadas.": "No actions recorded yet.",
    "¿Banear esta clave?\n{k}": "Ban this key?\n{k}",
    "Al permitir la primera clave, el relé pasa a ser de escritura restringida: solo podrán publicar las claves permitidas (y tú). ¿Continuar?": "Allowing the first key makes the relay write-restricted: only the allowed keys (and you) will be able to publish. Continue?",
    "Clave permitida": "Key allowed",
    "¿Vetar este evento? Se borra si está guardado y no podrá volver a publicarse.": "Veto this event? It is deleted if stored and can never be published again.",
    "Evento vetado (no estaba guardado)": "Event vetoed (it wasn't stored)",
    "Al permitir el primer tipo, SOLO pasarán los tipos permitidos (el resto se rechaza). ¿Continuar?": "Allowing the first kind means ONLY the allowed kinds will get through (the rest are rejected). Continue?",
    "Regla aplicada": "Rule applied",
    "IP bloqueada": "IP blocked",
    "No hay nada que guardar": "There is nothing to save",
    "Información guardada": "Information saved",
    "¿Restaurar toda la información del relé a los valores de la configuración?": "Restore all the relay information to the configuration values?",
    "Restaurados los de la configuración": "Restored to the configuration values",
    "Error al cargar el histórico: {e}": "Error loading the history: {e}",
    "Preparando la copia…": "Preparing the backup…",
    "Copia descargada: {n}": "Backup downloaded: {n}",
    "No se pudo descargar la copia: {e}": "Could not download the backup: {e}",
    "Buscar eventos de esta clave": "Search events of this key",
    "No se pudo buscar: {e}": "Could not search: {e}",
    "{n} resultado(s): {what}{range}": "{n} result(s): {what}{range}",
    "más de {n}": "more than {n}",
    "No hay eventos que coincidan.": "There are no matching events.",
    "Cargar más": "Load more",
    " · el {d}": " · on {d}",
    " · del {a} al {b}": " · from {a} to {b}",
    " · desde el {d}": " · since {d}",
    " · hasta el {d}": " · until {d}",
    "Tú (dueño del relé)": "You (relay owner)",
    "Clave sin perfil guardado": "Key with no stored profile",
    "dueño": "owner",
    "baneada": "banned",
    "en la lista blanca": "on the allow-list",
    "Quitar baneo": "Unban",
    "desde la búsqueda": "from the search",
    "Copiar el npub": "Copy the npub",
    "copiado ✓": "copied ✓",
    "{n} evento(s) guardados · el primero {a}, el último {b}": "{n} event(s) stored · the first {a}, the last {b}",
    "Sin eventos guardados": "No events stored",
    "Ver solo este tipo": "Show only this kind",
    "«Desde» no puede ser posterior a «Hasta»": "“From” can't be after “To”",
    "Enlace copiado": "Link copied",
    "No se pudo copiar el enlace": "Could not copy the link",
    "Conectando con {r}…": "Connecting to {r}…",
    "No se pudo abrir la conexión con {r}: {e}": "Could not open the connection to {r}: {e}",
    "{r} conectado ✓": "{r} connected ✓",
    "Conexión con {r} perdida; reintentando…": "Connection to {r} lost; retrying…",
    "Mensaje enviado a {r} ✓": "Message sent to {r} ✓",
    "{r} rechazó el mensaje: {m}": "{r} rejected the message: {m}",
    "sin motivo": "no reason",
    "Ha llegado un mensaje al panel que no se ha podido descifrar": "A message reached the panel that could not be decrypted",
    "Firmador emparejado ✓ (por {r})": "Signer paired ✓ (through {r})",
    "Ha llegado una respuesta del firmador, pero no con el secreto esperado": "A reply from the signer arrived, but not with the expected secret",
    "El navegador ha bloqueado algo por la política de seguridad: {d} ({u})": "The browser blocked something because of the security policy: {d} ({u})",
    "sin dirección": "no address",
    "no se ha aprobado la conexión a tiempo": "the connection wasn't approved in time",
    "el firmador aún no se ha conectado": "the signer hasn't connected yet",
    "el firmador no ha contestado a tiempo": "the signer didn't answer in time",
    "cancelado": "cancelled",
    "Perfil": "Profile",
    "Nota": "Note",
    "Contactos": "Contacts",
    "Mensaje directo (antiguo)": "Direct message (legacy)",
    "Borrado": "Deletion",
    "Repost": "Repost",
    "Reacción": "Reaction",
    "Seal (NIP-59)": "Seal (NIP-59)",
    "Mensaje directo (NIP-17)": "Direct message (NIP-17)",
    "Gift wrap (mensaje privado)": "Gift wrap (private message)",
    "Denuncia": "Report",
    "Zap": "Zap",
    "Lista de silenciados": "Mute list",
    "Lista de relés": "Relay list",
    "Relés de mensajes directos": "Direct-message relays",
    "Efímero 20001": "Ephemeral 20001",
    "Autenticación": "Authentication",
    "Auth HTTP": "HTTP auth",
    "Artículo": "Article",
    "Datos de app": "App data",
    "Quitada de la lista blanca": "Removed from the allow-list",
    "Evento vetado": "Event vetoed",
    "Tipo permitido": "Kind allowed",
    "Tipo prohibido": "Kind forbidden",
    "Regla de tipo quitada": "Kind rule removed",
    "Información del relé": "Relay information",
    "Copia de seguridad descargada": "Backup downloaded",
    "Inicio de sesión": "Login",
    "Cierre de sesión": "Logout",
    "· últimos 60 minutos": "· last 60 minutes",
    "· últimas 24 horas": "· last 24 hours",
    "· últimos 7 días": "· last 7 days",
    "· últimos 30 días": "· last 30 days",
    "· últimos 90 días": "· last 90 days",
    "eventos recientes": "recent events",
    "la búsqueda": "the search",
    "Panel del relé": "Relay panel",
  },
}
