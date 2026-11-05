// jsdom statt happy-dom: DOMPurify braucht ein vollständiges DOM (siehe richText.test.js).
// @vitest-environment jsdom
import { describe, it, expect } from 'vitest'
import { sanitizeMailPreview } from './mailPreview'

const render = (html) => {
  const box = document.createElement('div')
  box.innerHTML = sanitizeMailPreview(html)
  return box
}

describe('sanitizeMailPreview', () => {
  it('lässt das Mail-Markup der Standardvorlage stehen (Rahmen, Farben, Links, Listen)', () => {
    const box = render(
      '<div style="font-family:Arial,sans-serif;background:#f5f6f8;padding:32px 16px;">' +
      '<h2 style="margin:0 0 12px;color:#1a2230;">Titel</h2><p>Text <strong>fett</strong> <em>kursiv</em></p>' +
      '<ul><li>eins</li></ul><table><tbody><tr><td colspan="2">Zelle</td></tr></tbody></table>' +
      '<a href="https://status.example.com/x" style="display:inline-block;background:#4c6ef5;color:#ffffff;">Link</a></div>',
    )
    expect(box.querySelector('h2').textContent).toBe('Titel')
    expect(box.querySelector('strong')).not.toBeNull()
    expect(box.querySelector('li').textContent).toBe('eins')
    expect(box.querySelector('td').getAttribute('colspan')).toBe('2')
    const a = box.querySelector('a')
    expect(a.getAttribute('href')).toBe('https://status.example.com/x')
    expect(a.getAttribute('rel')).toContain('noopener')
    expect(a.getAttribute('target')).toBe('_blank')
    expect(box.querySelector('div').getAttribute('style')).toContain('background')
  })

  // Angriffsmatrix: nichts davon darf in der Admin-Oberfläche erscheinen.
  const attacks = {
    form: '<form action="https://evil.example.org/c" method="post">x</form>',
    input: '<input type="password" name="pw">',
    button: '<button>Anmelden</button>',
    textarea: '<textarea>x</textarea>',
    select: '<select><option>a</option></select>',
    img: '<img src="https://evil.example.org/b.gif">',
    'img onerror': '<img src=x onerror="window.__x=1">',
    svg: '<svg onload="window.__x=1"><circle/></svg>',
    math: '<math><mi>x</mi></math>',
    style: '<style>.a{position:fixed;inset:0}</style>',
    script: '<script>window.__x=1</script>',
    iframe: '<iframe src="https://evil.example.org"></iframe>',
    object: '<object data="https://evil.example.org"></object>',
    embed: '<embed src="https://evil.example.org">',
    link: '<link rel="stylesheet" href="https://evil.example.org/s.css">',
    meta: '<meta http-equiv="refresh" content="0;url=https://evil.example.org">',
    base: '<base href="https://evil.example.org/">',
    video: '<video src="https://evil.example.org/v.mp4"></video>',
    audio: '<audio src="https://evil.example.org/a.mp3"></audio>',
    details: '<details open ontoggle="window.__x=1"><summary>x</summary></details>',
  }
  for (const [name, payload] of Object.entries(attacks)) {
    it(`entfernt ${name}`, () => {
      const html = sanitizeMailPreview(`<p>vorher</p>${payload}<p>nachher</p>`)
      expect(html).toContain('vorher')
      expect(html).toContain('nachher')
      expect(html).not.toMatch(/<(form|input|button|textarea|select|option|img|svg|math|style|script|iframe|object|embed|link|meta|base|video|audio|details)\b/i)
      expect(html).not.toMatch(/\son\w+\s*=/i)
      expect(window.__x).toBeUndefined()
    })
  }

  it('entfernt Ereignis-Attribute und gefährliche Link-Ziele', () => {
    const box = render('<a href="javascript:alert(1)" onclick="window.__x=1">a</a><a href="data:text/html,x">b</a><a href="mailto:a@example.com">c</a><p onmouseover="window.__x=1">p</p>')
    const links = [...box.querySelectorAll('a')]
    expect(links[0].getAttribute('href')).toBeNull()
    expect(links[1].getAttribute('href')).toBeNull()
    expect(links[2].getAttribute('href')).toBe('mailto:a@example.com')
    expect(box.innerHTML).not.toMatch(/onclick|onmouseover/)
  })

  // Auf erlaubten Tags bleibt nur die feste Attribut-Allowlist. Insbesondere
  // Ereignis-Attribute mit einem Wert, der wie eine erlaubte URL aussieht
  // (die URL-Prüfung allein hielte sie nicht auf), sowie src/action, data-* und aria-*.
  it('entfernt nicht erlaubte Attribute auch auf erlaubten Tags', () => {
    const box = render(
      '<p onclick="https://evil.example.org" onmouseover="mailto:a@example.com" data-x="1" aria-label="x" class="a" id="b" title="t">p</p>' +
      '<a href="https://ok.example.org/" src="https://evil.example.org/s" action="https://evil.example.org/a" formaction="https://evil.example.org/f" ping="https://evil.example.org/p">l</a>',
    )
    const html = box.innerHTML
    expect(html).not.toMatch(/\son\w+\s*=/i)
    expect(html).not.toMatch(/\s(data-|aria-|class=|id=|title=|src=|action=|formaction=|ping=)/i)
    expect(box.querySelector('a').getAttribute('href')).toBe('https://ok.example.org/')
  })

  // !important würde ein erlaubtes Style-Attribut über die Oberfläche der Anwendung heben.
  it('entfernt !important aus Style-Angaben', () => {
    const box = render('<p style="color:red !important;font-weight:600">p</p>')
    const style = box.querySelector('p').getAttribute('style') || ''
    expect(style).not.toMatch(/important/i)
    expect(style).toContain('font-weight:600')
  })

  // Eine Overlay-Seite über die ganze Oberfläche (Phishing) per Inline-Style.
  it('entfernt positionierende und fremde Ressourcen ladende CSS-Angaben, behält Darstellung', () => {
    const box = render(
      '<div style="position:fixed;top:0;left:0;width:100vw;height:100vh;z-index:99999;background:#fff url(https://evil.example.org/b.png);color:#123456;padding:8px">Overlay</div>' +
      '<p style="background-image:url(https://evil.example.org/x);behavior:url(x);expression(1);font-weight:600">q</p>',
    )
    const [d, p] = box.children
    const ds = d.getAttribute('style') || ''
    expect(ds).not.toMatch(/position|z-index|url\(|100vw|100vh|top|left/i)
    expect(ds).toContain('color:#123456')
    expect(ds).toContain('padding:8px')
    const ps = p.getAttribute('style') || ''
    expect(ps).not.toMatch(/url\(|behavior|expression|background-image/i)
    expect(ps).toContain('font-weight:600')
  })

  // Ein Style, von dem nichts übrig bleibt, wird ganz entfernt, statt leer zu stehen.
  it('entfernt das style-Attribut, wenn keine erlaubte Angabe übrig bleibt', () => {
    const box = render('<p style="position:fixed;top:0;background:url(https://evil.example.org/x)">p</p>')
    expect(box.querySelector('p').hasAttribute('style')).toBe(false)
  })

  it('erlaubt nur harmlose display-Werte', () => {
    const box = render(
      '<div style="display:flex;color:#123456">a</div><div style="display:inline-block">b</div><div style="display:GRID">c</div>',
    )
    const [a, b, c] = box.children
    expect(a.getAttribute('style')).toBe('color:#123456')
    expect(b.getAttribute('style')).toBe('display:inline-block')
    expect(c.hasAttribute('style')).toBe(false)
  })

  it('behält start nur an geordneten Listen mit Ziffern', () => {
    const box = render(
      '<ol start="3"><li>a</li></ol><ol start="x3"><li>b</li></ol><ul start="3"><li>c</li></ul><p start="2">d</p>',
    )
    const [ok, badValue, ul, p] = box.children
    expect(ok.getAttribute('start')).toBe('3')
    expect(badValue.hasAttribute('start')).toBe(false)
    expect(ul.hasAttribute('start')).toBe(false)
    expect(p.hasAttribute('start')).toBe(false)
  })

  it('behandelt Leereingaben', () => {
    expect(sanitizeMailPreview('')).toBe('')
    expect(sanitizeMailPreview(undefined)).toBe('')
    expect(sanitizeMailPreview(null)).toBe('')
  })
})
