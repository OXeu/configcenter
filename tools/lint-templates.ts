import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { parse } from '@vue/compiler-sfc'
import { checkIconLabels } from './oxlint/icon-labels.ts'
import { checkTemplate } from './oxlint/templates.ts'
import { checkDangerTemplate } from './oxlint/danger.ts'
import { checkDropdownStyles } from './oxlint/dropdown.ts'

const catalogs = Object.fromEntries(
  readdirSync('internal/i18n/locales')
    .filter((name) => name.endsWith('.json'))
    .map((name) => [
      name.slice(0, -5),
      JSON.parse(readFileSync(join('internal/i18n/locales', name), 'utf8')) as Record<
        string,
        string
      >,
    ]),
)

function check(directory: string): void {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) check(path)
    else if (path.endsWith('.vue')) {
      const { descriptor } = parse(readFileSync(path, 'utf8'))
      const source = descriptor.template?.content ?? ''
      const errors = [
        ...checkTemplate(source, path),
        ...descriptor.styles.flatMap((style) => checkDropdownStyles(style.content)),
        ...checkDangerTemplate(source, path),
        ...checkIconLabels(source, descriptor.scriptSetup?.content ?? '', catalogs),
      ]
      if (errors.length) throw new Error(`${path}: ${errors.join(', ')}`)
    }
  }
}
check('web/ui/src')
const styleErrors = checkDropdownStyles(
  readFileSync('.tmp/common/ui/theme.css', 'utf8') + readFileSync('web/ui/theme.css', 'utf8'),
  true,
)
if (styleErrors.length) throw new Error(`web/ui/theme.css: ${styleErrors.join(', ')}`)
