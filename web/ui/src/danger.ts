import type { createForm } from './forms.ts'
export const dangerOperations = {
  signOut: 'nav.signOut',
  deleteUser: 'common.delete',
  rotateKey: 'projects.rotate',
  deleteProject: 'common.delete',
  deleteEnvironment: 'common.delete',
  renameEnvironment: 'environments.rename',
  deleteItem: 'common.delete',
  revokeKey: 'projects.revoke',
  approveProposal: 'projects.approve',
  rejectProposal: 'projects.reject',
} as const
export type DangerOperation = keyof typeof dangerOperations
const confirmedAction = Symbol('confirmedAction')
export type DangerAction = {
  [confirmedAction]: true
  id: DangerOperation
  label: string
  request: () => void
}
export function dangerAction(
  form: ReturnType<typeof createForm>,
  id: DangerOperation,
  label: string,
  target: string,
  execute: () => Promise<void>,
  options?: {
    fields?: Parameters<typeof form.show>[1]
    values?: Record<string, string>
    description?: string
  },
): DangerAction {
  return {
    [confirmedAction]: true,
    id,
    label,
    request() {
      form.show(`${label}: ${target}`, options?.fields ?? [], options?.values ?? {}, execute)
      form.description = options?.description ?? ''
      form.danger = true
    },
  }
}
