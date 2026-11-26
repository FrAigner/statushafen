import { afterEach } from 'vitest'
import { resetConfirm } from './confirm'

afterEach(() => resetConfirm())

localStorage.setItem('preferred_language', 'de')
// Test for SonarCloud coverage debugging

// happy-dom does not know window.confirm/alert/prompt. Vitest 3 still allowed vi.spyOn(window, 'confirm')
// (tinyspy spied on the undefined getter); since the spy rewrite in Vitest 4, spyOn aborts with
// "can only spy on a function. Received undefined". Hence browser-like defaults here
// (an unanswered dialog yields false or null), which the tests override specifically via spyOn.
if (typeof window.confirm !== 'function') window.confirm = () => false
if (typeof window.alert !== 'function') window.alert = () => {}
if (typeof window.prompt !== 'function') window.prompt = () => null
