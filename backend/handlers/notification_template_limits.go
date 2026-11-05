package handlers

import (
	"bytes"
	"errors"
	htmltemplate "html/template"
	"reflect"
	"text/template/parse"
	"time"
)

// Grenzen der Vorlagen-Engine. Vorlagen kommen von Seiten-Admins und werden
// beim Versand je Empfänger ausgeführt; ohne Grenzen ließe sich mit wenigen
// Zeichen (Schleifen, printf mit Breite, Unterschablonen) der ganze Prozess
// auslasten. Deshalb sind nur Feldzugriffe auf die Platzhalter und einfache
// Bedingungen erlaubt, und Länge, Ausgabe und Laufzeit sind begrenzt.
const (
	// MaxTemplateSubjectLen und MaxTemplateBodyLen begrenzen den Quelltext
	// der Vorlage in Bytes.
	MaxTemplateSubjectLen = 500
	MaxTemplateBodyLen    = 64 * 1024

	maxTemplateSubjectOutput = 4 * 1024
	maxTemplateBodyOutput    = 512 * 1024
)

// templateExecTimeout ist die Obergrenze für das Rendern einer Vorlage.
// Variable statt Konstante, damit Tests sie verkürzen können.
var templateExecTimeout = 2 * time.Second

// allowedTemplateFuncs sind die einzigen Funktionen in Bedingungen
// ({{if eq .Status "resolved"}}); sie erzeugen selbst keine Ausgabe.
var allowedTemplateFuncs = map[string]bool{"eq": true, "ne": true, "not": true, "and": true, "or": true}

// allowedTemplateFields sind die Platzhalter, die eine Vorlage lesen darf.
var allowedTemplateFields = func() map[string]bool {
	m := map[string]bool{}
	t := reflect.TypeOf(NotificationTemplateData{})
	for i := 0; i < t.NumField(); i++ {
		m[t.Field(i).Name] = true
	}
	return m
}()

// TemplateErrorCode liefert den Fehlercode eines Vorlagen-Prüffehlers
// (leer, wenn es keiner ist).
func TemplateErrorCode(err error) string {
	var ce *codedError
	if errors.As(err, &ce) {
		return ce.code
	}
	return ""
}

func errTemplateOutputTooLarge() error {
	return newCodedError("template_output_too_large", "Die Vorlage erzeugt eine zu große Ausgabe")
}

func errTemplateNotAllowed(what string) error {
	return newCodedError("template_construct_not_allowed", "In Vorlagen sind nur Platzhalter und einfache Bedingungen erlaubt: "+what)
}

// parseAllowedTemplate parst eine Vorlage und lehnt zu lange Quelltexte sowie
// alle Konstrukte außerhalb der Allowlist ab.
func parseAllowedTemplate(name, raw string, maxLen int) (*htmltemplate.Template, error) {
	t, _, err := parseAllowedTemplateFields(name, raw, maxLen)
	return t, err
}

// parseAllowedTemplateFields wie parseAllowedTemplate; liefert zusätzlich die
// Platzhalter, die die Vorlage unbedingt (außerhalb von Bedingungen) ausgibt.
func parseAllowedTemplateFields(name, raw string, maxLen int) (*htmltemplate.Template, map[string]bool, error) {
	if len(raw) > maxLen {
		return nil, nil, newCodedError("template_too_long", "Die Vorlage ist zu lang")
	}
	t, err := htmltemplate.New(name).Parse(raw)
	if err != nil {
		return nil, nil, newCodedError("template_invalid", "Vorlage ungültig: "+err.Error())
	}
	if len(t.Templates()) > 1 {
		return nil, nil, errTemplateNotAllowed("define/block")
	}
	used := map[string]bool{}
	// Nach erfolgreichem Parsen hat die Vorlage immer einen Baum mit Wurzel
	// (auch die leere), eine nil-Prüfung wäre unerreichbar.
	if err := checkTemplateNode(t.Tree.Root, 0, used); err != nil {
		return nil, nil, err
	}
	return t, used, nil
}

// checkTemplateNode prüft einen Knoten gegen die Allowlist. In used landen die
// Platzhalter, die auf oberster Ebene (depth 0, nicht in einer Bedingung)
// ausgegeben werden.
func checkTemplateNode(n parse.Node, depth int, used map[string]bool) error {
	switch v := n.(type) {
	case nil:
		return nil
	case *parse.ListNode:
		if v == nil {
			return nil
		}
		for _, c := range v.Nodes {
			if err := checkTemplateNode(c, depth, used); err != nil {
				return err
			}
		}
		return nil
	case *parse.TextNode, *parse.CommentNode:
		return nil
	case *parse.ActionNode:
		if len(v.Pipe.Decl) > 0 || len(v.Pipe.Cmds) != 1 || len(v.Pipe.Cmds[0].Args) != 1 {
			return errTemplateNotAllowed("Pipeline")
		}
		if err := checkTemplateOperand(v.Pipe.Cmds[0].Args[0], false); err != nil {
			return err
		}
		if f, ok := v.Pipe.Cmds[0].Args[0].(*parse.FieldNode); ok && depth == 0 {
			used[f.Ident[0]] = true
		}
		return nil
	case *parse.IfNode:
		if err := checkTemplateCondition(v.Pipe); err != nil {
			return err
		}
		if err := checkTemplateNode(v.List, depth+1, used); err != nil {
			return err
		}
		return checkTemplateNode(v.ElseList, depth+1, used)
	default:
		return errTemplateNotAllowed(n.String())
	}
}

// checkTemplateCondition prüft die Pipeline einer Bedingung.
func checkTemplateCondition(p *parse.PipeNode) error {
	if p == nil || len(p.Decl) > 0 || len(p.Cmds) != 1 {
		return errTemplateNotAllowed("Pipeline")
	}
	cmd := p.Cmds[0]
	if len(cmd.Args) == 0 {
		return errTemplateNotAllowed("Pipeline")
	}
	if id, ok := cmd.Args[0].(*parse.IdentifierNode); ok {
		if !allowedTemplateFuncs[id.Ident] {
			return errTemplateNotAllowed("Funktion " + id.Ident)
		}
		for _, a := range cmd.Args[1:] {
			if err := checkTemplateOperand(a, true); err != nil {
				return err
			}
		}
		return nil
	}
	if len(cmd.Args) != 1 {
		return errTemplateNotAllowed("Aufruf")
	}
	return checkTemplateOperand(cmd.Args[0], true)
}

// checkTemplateOperand erlaubt Platzhalter, in Bedingungen zusätzlich
// Literale und Klammerausdrücke.
func checkTemplateOperand(n parse.Node, cond bool) error {
	switch v := n.(type) {
	case *parse.FieldNode:
		if len(v.Ident) != 1 || !allowedTemplateFields[v.Ident[0]] {
			return newCodedError("template_invalid", "Unbekannter Platzhalter: "+v.String())
		}
		return nil
	case *parse.StringNode, *parse.BoolNode, *parse.NumberNode:
		if cond {
			return nil
		}
	case *parse.PipeNode:
		if cond {
			return checkTemplateCondition(v)
		}
	}
	return errTemplateNotAllowed(n.String())
}

// limitedWriter bricht ab, sobald mehr als max Bytes geschrieben werden.
type limitedWriter struct {
	buf      bytes.Buffer
	max      int
	exceeded bool
}

type outputLimitError struct{}

func (outputLimitError) Error() string { return "template output limit exceeded" }

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.max {
		w.exceeded = true
		return 0, outputLimitError{}
	}
	return w.buf.Write(p)
}

// runWithTimeout führt fn aus und bricht das Warten nach d ab. Die
// Ausgabegrenze beendet eine weiterlaufende Ausführung ohnehin zeitnah.
func runWithTimeout(d time.Duration, fn func() (string, error)) (string, error) {
	type result struct {
		s   string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		s, err := fn()
		ch <- result{s, err}
	}()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case r := <-ch:
		return r.s, r.err
	case <-timer.C:
		return "", newCodedError("template_timeout", "Die Vorlage braucht zu lange zum Rendern")
	}
}
