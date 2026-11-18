export function formatJson(value: string): string {
  JSON.parse(value)
  // Keep string escapes, number literals and member order exactly as entered.
  const tokens = value.match(/"(?:[^"\\]|\\.)*"|[{}[\],:]|[^{}[\],:" \t\r\n]+/gu) ?? []
  const output: string[] = []
  let depth = 0
  const line = () => '\n' + '  '.repeat(depth)

  for (const [index, token] of tokens.entries()) {
    switch (token) {
      case '{':
      case '[': {
        output.push(token)
        depth++
        const next = tokens[index + 1]
        if (next !== '}' && next !== ']') output.push(line())
        break
      }
      case '}':
      case ']': {
        depth--
        const previous = tokens[index - 1]
        if (previous !== '{' && previous !== '[') output.push(line())
        output.push(token)
        break
      }
      case ':':
        output.push(': ')
        break
      case ',':
        output.push(',', line())
        break
      default:
        output.push(token)
    }
  }
  return output.join('')
}
