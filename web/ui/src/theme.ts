import { useTheme as commonTheme } from '@oxeu/center-common/ui/theme.ts'
export type { Theme } from '@oxeu/center-common/ui/theme.ts'

export function useTheme() {
  return commonTheme('cc_theme')
}
