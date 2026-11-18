import dark from '@shikijs/themes/github-dark'
import light from '@shikijs/themes/github-light'
import { createHighlighterCore } from 'shiki/core'
import { createJavaScriptRegexEngine } from 'shiki/engine/javascript'

const grammars = {
  json: () => import('@shikijs/langs/json'),
  yaml: () => import('@shikijs/langs/yaml'),
  javascript: () => import('@shikijs/langs/javascript'),
}
export type Language = keyof typeof grammars

const highlighter = createHighlighterCore({
  engine: createJavaScriptRegexEngine(),
  themes: [light, dark],
})

export async function editorHighlight(lang: Language) {
  const editor = await highlighter
  if (!editor.getLoadedLanguages().includes(lang)) await editor.loadLanguage(grammars[lang])
  return (value: string) =>
    editor
      .codeToTokensWithThemes(value.replace(/\r\n?/gu, '\n'), {
        lang,
        themes: { light: 'github-light', dark: 'github-dark' },
        tokenizeMaxLineLength: 8192,
        tokenizeTimeLimit: 30,
      })
      .map((line) =>
        line.map(({ content, variants }) => ({
          content,
          colors: { '--token-light': variants.light.color, '--token-dark': variants.dark.color },
        })),
      )
}
