# Was ist Statushafen

Statushafen ist eine Plattform für Statusseiten. Unternehmen zeigen darauf, ob ihre Dienste
laufen, melden Störungen, kündigen Wartungen an und benachrichtigen Interessierte per E-Mail
oder Google Chat.

## Die wichtigsten Bausteine

- **Statusseiten:** Jede Statusseite ist ein eigener Mandant mit eigener Adresse, zum Beispiel
  `acme.example.com` oder `/status/acme`. Siehe [Statusseiten und Mandanten](/docs/statusseiten).
- **Komponenten:** die Dienste, deren Zustand die Seite zeigt, etwa „Website“ oder „API“.
  Siehe [Komponenten und Tags](/docs/komponenten-und-tags).
- **Externe Quellen:** fremde Statusseiten (z. B. eines Cloud-Anbieters), deren Zustand eigene
  Komponenten mitbestimmt. Siehe [Externe Quellen](/docs/externe-quellen).
- **Vorfälle:** Störungen mit Status-Verlauf und Updates. Siehe [Vorfälle](/docs/vorfaelle).
- **Wartungsfenster:** geplante Arbeiten, die automatisch starten und enden.
  Siehe [Wartungsfenster](/docs/wartungsfenster).
- **Ankündigungen:** allgemeine Hinweise auf der Statusseite.
  Siehe [Ankündigungen](/docs/ankuendigungen).
- **Abonnements:** Benachrichtigungen per E-Mail (mit Double-Opt-in) oder Google-Chat-Webhook.
  Siehe [Abonnements](/docs/abonnements).

## Öffentliche Seite und Admin-Bereich

Die **öffentliche Statusseite** zeigt den Gesamtstatus, alle Komponenten, die Verfügbarkeit der
letzten 90 Tage, laufende und geplante Wartungen sowie vergangene Vorfälle. Ist eine Seite als
privat markiert, sehen sie nur angemeldete Nutzer mit Zugriff. Was die Besucher sehen und wie sie
suchen und filtern, steht unter [Die öffentliche Statusseite](/docs/oeffentliche-statusseite).

Der **Admin-Bereich** liegt unter `/admin`. Dort pflegt man Komponenten, externe Quellen,
Vorfälle, Wartungen, Ankündigungen, Vorlagen und Abonnenten. Was ein Nutzer sieht, hängt von
seiner Rolle ab, siehe [Benutzer, Rollen und Rechte](/docs/benutzer-und-rollen). Aufbau und
Bedienung des Admin-Bereichs beschreibt [Erste Schritte](/docs/erste-schritte#der-admin-bereich).

## Startseite

Die Startseite von Statushafen listet unter „Aktuelle Statusseiten“ alle Seiten, die öffentlich
und nicht privat sind. Ist die Self-Service-Bestellung eingeschaltet, steht dort außerdem der
Knopf „Eigene Statusseite bestellen“, siehe [Erste Schritte](/docs/erste-schritte).
