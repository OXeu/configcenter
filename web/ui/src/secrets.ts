import { reactive } from 'vue'

export type SecretRow = { key: string; id?: number; value?: string; secret?: boolean }

export function createSecretViews(
  read: (row: SecretRow) => Promise<string>,
  report: (error: unknown) => void,
) {
  const values = reactive(new Map<SecretRow, string>())
  const pending = reactive(new Set<SecretRow>())
  let generation = 0
  function reset() {
    generation++
    values.clear()
    pending.clear()
  }
  async function toggle(row: SecretRow) {
    if (values.has(row)) {
      values.delete(row)
      return
    }
    if (pending.has(row)) return
    const current = generation
    pending.add(row)
    try {
      const value = await read(row)
      if (current === generation) values.set(row, value)
    } catch (error) {
      if (current === generation) report(error)
    } finally {
      if (current === generation) pending.delete(row)
    }
  }
  return { values, pending, toggle, reset }
}
