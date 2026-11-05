#!/usr/bin/env python3
"""Erzeugt THIRD_PARTY_NOTICES.md aus den Lizenzen der Laufzeit-Abhängigkeiten.

Nur Standardbibliothek. Aufruf aus der Repo-Wurzel (nach `npm ci` im frontend/):

    GOBIN=/tmp/bin go install github.com/google/go-licenses/v2@latest
    (cd backend && /tmp/bin/go-licenses report ./... --ignore github.com/fraigner/statushafen > /tmp/go-report.csv)
    (cd backend && /tmp/bin/go-licenses save   ./... --ignore github.com/fraigner/statushafen --save_path /tmp/go-licenses)
    python3 scripts/third_party_notices.py /tmp/go-report.csv /tmp/go-licenses > THIRD_PARTY_NOTICES.md

Go: Bericht (CSV) und gesammelte Lizenzdateien von go-licenses. npm: Laufzeitpakete
(ohne dev) aus frontend/package-lock.json, Lizenztexte aus frontend/node_modules.
"""
import csv
import json
import os
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
FRONTEND = os.path.join(ROOT, "frontend")
LICENSE_PREFIXES = ("license", "licence", "copying", "notice")
FONT_PACKAGES = ("@fontsource/inter", "@fontsource/ibm-plex-sans", "@fontsource/ibm-plex-mono")


def read(path):
    with open(path, encoding="utf-8", errors="replace") as f:
        return f.read().strip("\n")


def license_files(directory):
    """Lizenz- und NOTICE-Dateien direkt in directory, sortiert."""
    if not os.path.isdir(directory):
        return []
    names = [n for n in os.listdir(directory)
             if n.lower().startswith(LICENSE_PREFIXES) and os.path.isfile(os.path.join(directory, n))]
    return [os.path.join(directory, n) for n in sorted(names)]


def go_section(report_csv, save_dir):
    rows = []
    with open(report_csv, encoding="utf-8") as f:
        for name, _url, lic in csv.reader(f):
            rows.append((name, lic))
    rows.sort()
    table = ["| Paket | Lizenz |", "|---|---|"] + ["| `%s` | %s |" % r for r in rows]
    texts = []
    for dirpath, _dirs, _files in sorted(os.walk(save_dir)):
        for path in license_files(dirpath):
            rel = os.path.relpath(path, save_dir)
            texts.append("### Go: `%s`\n\n```text\n%s\n```" % (rel, read(path)))
    return table, texts


def npm_runtime():
    lock = json.load(open(os.path.join(FRONTEND, "package-lock.json"), encoding="utf-8"))
    out = {}
    for key, meta in lock["packages"].items():
        if key.startswith("node_modules/") and not meta.get("dev") and not meta.get("devOptional"):
            out[key.split("node_modules/", 1)[1]] = meta
    return dict(sorted(out.items()))


def npm_section():
    pkgs = npm_runtime()
    table = ["| Paket | Version | Lizenz |", "|---|---|---|"]
    texts = []
    for name, meta in pkgs.items():
        table.append("| `%s` | %s | %s |" % (name, meta.get("version", ""), meta.get("license", "")))
        directory = os.path.join(FRONTEND, "node_modules", name)
        files = license_files(directory)
        for path in files:
            texts.append("### npm: `%s` (%s)\n\n```text\n%s\n```" % (name, os.path.basename(path), read(path)))
        if not files:
            texts.append("### npm: `%s`\n\nKeine Lizenzdatei im Paket; Lizenz laut package.json: %s." % (name, meta.get("license", "unbekannt")))
    return table, texts


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    go_table, go_texts = go_section(sys.argv[1], sys.argv[2])
    npm_table, npm_texts = npm_section()
    out = [
        "# Third-party notices",
        "",
        "Statushafen steht unter der MIT-Lizenz (siehe [LICENSE](LICENSE)). Die Software enthält oder nutzt die",
        "folgenden Drittkomponenten. Sie behalten ihre eigenen Lizenzen; die Hinweise und Lizenztexte stehen",
        "unten, damit sie bei der Weitergabe (Quellcode, Build, Container-Image) mitgeliefert werden.",
        "Diese Datei wird mit `scripts/third_party_notices.py` erzeugt.",
        "",
        "## Schriften (SIL Open Font License 1.1)",
        "",
        "Inter, IBM Plex Sans und IBM Plex Mono werden aus den npm-Paketen `@fontsource/*` gebaut und mit",
        "der Anwendung ausgeliefert. Die Schriften stehen unter der SIL Open Font License 1.1, nicht unter der",
        "MIT-Lizenz. Copyright-Zeilen und Lizenztext je Schrift folgen im Abschnitt npm.",
        "",
        "## Dual-Lizenz: DOMPurify",
        "",
        "DOMPurify steht unter \"MPL-2.0 OR Apache-2.0\". Statushafen nutzt es unter der Apache-2.0-Lizenz.",
        "",
        "## Go-Abhängigkeiten (Backend)",
        "",
    ] + go_table + ["", "## npm-Abhängigkeiten (Frontend, Laufzeit)", ""] + npm_table + [
        "", "## Lizenztexte", "",
    ]
    out += [t + "\n" for t in npm_texts] + [t + "\n" for t in go_texts]
    sys.stdout.write("\n".join(out).rstrip("\n") + "\n")


if __name__ == "__main__":
    main()
