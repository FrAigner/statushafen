import { afterEach } from 'vitest'
import { resetConfirm } from './confirm'

afterEach(() => resetConfirm())

localStorage.setItem('preferred_language', 'de')
// Test für SonarCloud Coverage Debug

// happy-dom kennt window.confirm/alert/prompt nicht. Vitest 3 ließ vi.spyOn(window, 'confirm')
// trotzdem zu (tinyspy spionierte auf den undefined-Getter), seit der Spy-Neufassung in Vitest 4 bricht
// spyOn mit "can only spy on a function. Received undefined" ab. Deshalb hier browserähnliche Vorgaben
// (ein nicht beantworteter Dialog liefert false bzw. null), die die Tests per spyOn gezielt überschreiben.
if (typeof window.confirm !== 'function') window.confirm = () => false
if (typeof window.alert !== 'function') window.alert = () => {}
if (typeof window.prompt !== 'function') window.prompt = () => null
