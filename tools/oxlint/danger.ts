import { checkDangerTemplate as commonDanger } from '@oxeu/center-common/tools/oxlint/danger.ts'
import { dangerOperations } from '../../web/ui/src/danger.ts'

export function checkDangerTemplate(source: string, filename = ''): string[] {
  return commonDanger(source, filename, dangerOperations, {})
}
