# Security Policy

Deutsch zuerst, English below.

## Deutsch

### Unterstützte Versionen

Sicherheitsfixes gibt es für den jeweils aktuellen Stand des Hauptzweigs (`main`). Ältere Versionen
werden nicht gepflegt; aktualisiere bitte auf den neuesten Stand.

### Schwachstelle melden

Melde Sicherheitslücken **nicht** als öffentliches Issue. Nutze die private Meldung von GitHub:
im Repository auf **Security → Report a vulnerability**. Beschreibe, was betroffen ist, wie man es
nachstellt und welche Auswirkung du siehst; Beispieldaten ohne echte Zugangsdaten genügen.

Du bekommst in der Regel innerhalb weniger Tage eine erste Antwort. Bestätigte Lücken werden
behoben und mit einem Hinweis im Änderungsverlauf veröffentlicht; wenn du möchtest, nennen wir dich
als Melder. Bitte gib uns angemessen Zeit für den Fix, bevor du Details veröffentlichst.

### Hinweise für Betreiber

- Setze in Produktion `APP_ENV=production`, ein starkes `JWT_SECRET` und niemals `SEED_DB`.
- Die Demo-Konten (`*@statushafen.local`) gibt es nur mit `SEED_DB=true` und `APP_ENV=development`,
  `dev`, `test` oder `local` (jeder andere oder leere Wert sperrt, das Backend loggt eine Warnung);
  sie sind nur für lokale Entwicklung gedacht.
- Betreibe die Anwendung hinter TLS und setze `TRUSTED_PROXIES` auf die Adressen deines Proxys.

## English

### Supported versions

Security fixes are provided for the current state of the main branch (`main`). Older versions are not
maintained; please update to the latest state.

### Reporting a vulnerability

Please do **not** report vulnerabilities as public issues. Use GitHub's private reporting: in the
repository choose **Security → Report a vulnerability**. Describe what is affected, how to reproduce
it and the impact you see; sample data without real credentials is enough.

You will usually get a first answer within a few days. Confirmed vulnerabilities are fixed and
announced with a note in the change history; we credit you as the reporter if you wish. Please give us
reasonable time to fix before you publish details.

### Notes for operators

- In production set `APP_ENV=production`, a strong `JWT_SECRET` and never `SEED_DB`.
- The demo accounts (`*@statushafen.local`) only exist with `SEED_DB=true` and `APP_ENV=development`,
  `dev`, `test` or `local` (any other or empty value blocks it and the backend logs a warning); they
  are for local development only.
- Run the application behind TLS and set `TRUSTED_PROXIES` to the addresses of your proxy.
