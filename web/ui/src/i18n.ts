import en from '../../../internal/i18n/locales/en.json'
import zh from '../../../internal/i18n/locales/zh-CN.json'

/** @type {Record<string, Record<string, string>>} */
export const fallbackMessages: Readonly<Record<string, Readonly<Record<string, string>>>> = {
  en,
  'zh-CN': zh,
}

export type Copy = { msg: (id: string, vars?: Record<string, string>) => string; lang: string }

/**
 * @param {string} lang
 * @param {Record<string, string>} [serverMessages]
 * @returns {Copy}
 */
export function createCopy(lang: string, serverMessages: Record<string, string> = {}): Copy {
  const table = { ...fallbackMessages.en, ...fallbackMessages[lang], ...serverMessages }
  /**
   * @param {string} id
   * @param {Record<string, string>} [vars]
   * @returns {string}
   */
  const msg = (id: string, vars: Record<string, string> = {}): string =>
    (table[id] ?? id).replace(
      /\{\{\.?(\w+)\}\}/gu,
      (_, /** @type {string} */ name: string) => vars[name] ?? '',
    )
  return { msg, lang }
}
