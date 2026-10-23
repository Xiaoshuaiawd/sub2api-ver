import { describe, expect, it } from 'vitest'
import { buildVcodeCallbackUrl } from '../vcodeAuthorize'

describe('Vcode authorization callback', () => {
  it('uses only the fixed control origin and validated state/code', () => {
    const state = 'a'.repeat(43)
    const code = 'b'.repeat(43)
    expect(buildVcodeCallbackUrl('https://agent.albkw.cn', state, code)).toBe(
      `https://agent.albkw.cn/api/v1/control/auth/callback?state=${state}&code=${code}`
    )
    expect(() => buildVcodeCallbackUrl('https://evil.example/path', state, code)).toThrow()
    expect(() => buildVcodeCallbackUrl('https://agent.albkw.cn', '../evil', code)).toThrow()
  })
})
