export type User = {
  id: number
  name: string
  avatar_url?: string
  email: string
  role: string
  last_login_at?: string | null
}

export type PageData = {
  page: 'login' | 'projects' | 'project' | 'users' | 'settings' | 'keys' | 'docs' | 'oauth-consent'
  consent?: { request: string; clientName: string; redirectUri: string; resource: string }
  docPage?: string
  lang: string
  title: string
  publicUrl: string
  providerName: string
  nonce: string
  csrf: string
  loggedIn: boolean
  isAdmin: boolean
  oauthEnabled: boolean
  me: User | null
  project: { slug: string; name: string } | null
  env: string
  canWrite: boolean
  languageLabel: string
  languages: { tag: string; label: string }[]
  messages: Record<string, string>
  flash: { message: string; level: 'error' | 'success' } | null
}

export const pages = [
  'login',
  'projects',
  'project',
  'users',
  'settings',
  'keys',
  'docs',
  'oauth-consent',
]

export function parsePageData(value: unknown): PageData {
  if (
    !value ||
    typeof value !== 'object' ||
    !('page' in value) ||
    typeof value.page !== 'string' ||
    !pages.includes(value.page) ||
    !('nonce' in value) ||
    typeof value.nonce !== 'string' ||
    !('csrf' in value) ||
    typeof value.csrf !== 'string'
  )
    throw new Error('Invalid page data')
  return value as PageData
}
