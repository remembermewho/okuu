import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

// React Router создаёт Request с AbortSignal из jsdom, а Request из Node (undici) принимает
// только свой AbortSignal. В тестах отмена навигаций не нужна — signal не передаём.
const NativeRequest = globalThis.Request
class TestRequest extends NativeRequest {
  constructor(input: RequestInfo | URL, init?: RequestInit) {
    if (init?.signal) {
      const { signal: _signal, ...rest } = init
      super(input, rest)
    } else {
      super(input, init)
    }
  }
}
globalThis.Request = TestRequest as typeof Request

afterEach(() => {
  cleanup()
})
