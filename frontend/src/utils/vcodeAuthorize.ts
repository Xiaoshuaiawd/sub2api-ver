const statePattern = /^[A-Za-z0-9_-]{43}$/

export function buildVcodeCallbackUrl(origin: string, state: string, code: string): string {
  const target = new URL(origin)
  const loopback = ['localhost', '127.0.0.1', '[::1]'].includes(target.hostname)
  if (
    (target.protocol !== 'https:' && !(target.protocol === 'http:' && loopback)) ||
    target.pathname !== '/' || target.search || target.hash || target.username || target.password ||
    !statePattern.test(state) || !statePattern.test(code)
  ) {
    throw new Error('Invalid Vcode authorization callback')
  }
  target.pathname = '/api/v1/control/auth/callback'
  target.searchParams.set('state', state)
  target.searchParams.set('code', code)
  return target.toString()
}
