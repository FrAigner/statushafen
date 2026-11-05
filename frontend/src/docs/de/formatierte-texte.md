# Formatierte Texte

Nachrichten und Beschreibungen lassen sich formatieren: fett, kursiv, Listen, Absätze und Links.
Das gilt für Vorfälle und deren Updates, Wartungsfenster, Ankündigungen sowie die Vorlagen für
Vorfälle und Wartungen (jeweils das Feld „Nachricht / Details“, nicht der Titel). Die Formatierung
wird als eingeschränktes Markdown gespeichert und geschrieben. Das Formularfeld hat eine
Werkzeugleiste, man muss die Zeichen also nicht selbst tippen (siehe [Editor](#editor)).

## Editor

Über dem Feld stehen Knöpfe für fett, kursiv, Aufzählung, Zitat, Code und Link (nur https). Sie
setzen die Markdown-Zeichen um den markierten Text oder an die Cursorstelle; ein zweiter Klick auf
fett, kursiv oder Code nimmt sie wieder zurück. Mit Strg+B (am Mac Cmd+B) und Strg+I lassen sich
fett und kursiv auch per Tastatur setzen. Nach „Link“ ist die Adresse `https://` markiert und kann
direkt überschrieben werden.

„Vorschau“ zeigt den Text genau so, wie ihn die Statusseite darstellt, mit derselben Bereinigung:
Roh-HTML, Bilder und Überschriften erscheinen auch dort als Text. „Bearbeiten“ führt zurück. Unter
dem Feld zählt ein Zähler die Zeichen gegen die Grenze von 5.000; ist der Text länger, wird der
Zähler rot und das Speichern wird abgelehnt.

## Schreibweise

| Eingabe | Ergebnis |
|---|---|
| `**fett**` | **fett** |
| `*kursiv*` oder `_kursiv_` | *kursiv* |
| `- Punkt` (eine Zeile je Punkt) | Aufzählung |
| `1. Schritt` (eine Zeile je Schritt) | nummerierte Liste |
| `> Zitat` | eingerückter Hinweis |
| `` `Code` `` | Text in Schreibmaschinenschrift |
| `[Text](https://beispiel.de)` oder eine nackte `https://…`-Adresse | Link |
| Leerzeile | neuer Absatz |

Ein einfacher Zeilenumbruch bleibt ein Zeilenumbruch.

```text
Wir untersuchen **Fehler beim Anmelden**.

Betroffen:
- Login
- Passwort zurücksetzen

Details: [Statusseite](https://status.beispiel.de)
```

## Was nicht dargestellt wird

Überschriften, Bilder, Tabellen und eingebettetes HTML gehören nicht dazu. Solche Schreibweisen
erscheinen unverändert als Text; HTML wird nie ausgeführt. Links sind nur mit `https:` möglich;
eine `http:`-, `javascript:`- oder `data:`-Adresse wird nicht zum Link, sondern als Text angezeigt.
Links öffnen in einem neuen Tab und tragen `rel="noopener noreferrer nofollow"`.

## Bestehende Texte

Texte, die bisher ohne Formatierung geschrieben wurden, bleiben lesbar: Sie werden nicht verändert,
einfache Zeilenumbrüche bleiben erhalten. Zeichen wie `*` oder `_` mitten im Text oder ein `#` am
Zeilenanfang erscheinen weiter als normaler Text.

## Wo die Formatierung wirkt

- **Statusseite** (alle Layouts): formatiert dargestellt, auch im Verlauf eines Vorfalls.
- **E-Mail**: Standardmails zeigen die formatierte Fassung als HTML. Der Betreff bleibt Klartext.
- **Eigene Mail-Vorlagen**: `{{.MessageHTML}}` liefert die formatierte Fassung, `{{.Message}}`
  weiterhin den unformatierten Text, siehe [Platzhalter](/docs/mail-templates#platzhalter).
- **Chat und Webhook (Google Chat)**: Klartext, Links als „Text (Adresse)“, Listen mit „- “.
- Meldungen [externer Quellen](/docs/externe-quellen) bleiben Klartext.

## Länge

Eine Nachricht darf höchstens 5.000 Zeichen lang sein, gezählt wird der geschriebene Text samt
Formatierungszeichen. Längere Texte lehnt Statushafen beim Speichern mit einer Fehlermeldung ab.

Ältere, längere Texte (über 8.000 Zeichen) oder einzelne Absätze über 2.000 Zeichen ohne Leerzeile
zeigt Statushafen unverändert als Text ohne Formatierung an, damit die Seite schnell bleibt.

Auch das Formatieren vieler Texte auf einer Seite hat ein Zeitbudget: Sehr lange oder mit Formatierzeichen
überladene Texte brauchen mehr davon als kurze Meldungen. Ist das Budget aufgebraucht, erscheinen weitere
solche Texte vorübergehend ohne Formatierung (vollständig, nichts wird abgeschnitten); nach kurzer Zeit
werden sie wieder formatiert. Übliche Vorfallmeldungen sind davon nicht betroffen.
