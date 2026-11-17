import { execFileSync } from 'node:child_process'
import {
  existsSync,
  lstatSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  realpathSync,
  rmSync,
  symlinkSync,
  unlinkSync,
  writeFileSync,
} from 'node:fs'
import { resolve, join } from 'node:path'

const root = process.cwd()
const args = process.argv.slice(2)
if (
  args.length &&
  !(args.length === 1 && args[0] === '--locked') &&
  !(args.length === 2 && args[0] === '--local')
) {
  throw new Error('Usage: node tools/common.ts [--locked | --local PATH]')
}
const lock = JSON.parse(readFileSync(join(root, 'common.lock.json'), 'utf8'))
if (
  !/^[a-f0-9]{40}$/u.test(lock.revision) ||
  typeof lock.repository !== 'string' ||
  !lock.repository
) {
  throw new Error('common.lock.json must contain a repository and a full commit SHA')
}
const scratch = join(root, '.tmp')
mkdirSync(scratch, { recursive: true })
const local = join(scratch, 'common-local')
const active = join(scratch, 'common')
const quote = (value: string) => "'" + value.replaceAll("'", "'\\''") + "'"
function link(target: string, path: string) {
  const stat = lstatSync(path, { throwIfNoEntry: false })
  if (stat && !stat.isSymbolicLink()) throw new Error(`Refusing to replace non-symlink: ${path}`)
  if (stat) unlinkSync(path)
  symlinkSync(target, path, 'dir')
}
function validate(path: string) {
  const manifest = JSON.parse(readFileSync(join(path, 'package.json'), 'utf8'))
  if (manifest.name !== '@oxeu/center-common' || !existsSync(join(path, 'go.mod'))) {
    throw new Error('Expected a center-common checkout')
  }
}
if (args[0] === '--local') {
  if (process.env.CI) throw new Error('Local common overrides are disabled in CI')
  const target = realpathSync(resolve(root, args[1]))
  validate(target)
  link(target, local)
}
let target: string
if (args[0] !== '--locked' && !process.env.CI && existsSync(local)) {
  target = realpathSync(local)
  validate(target)
} else {
  target = join(scratch, 'common-cache', lock.revision)
  mkdirSync(target, { recursive: true })
  const git = (...command: string[]) => {
    const env = { ...process.env }
    let credentials: string | undefined
    try {
      if (
        env.COMMON_REPO_SSH_KEY &&
        command[0] === 'fetch' &&
        lock.repository.startsWith('https://github.com/')
      ) {
        credentials = mkdtempSync(join(scratch, 'common-ssh-'))
        const key = join(credentials, 'key')
        const hosts = join(credentials, 'known_hosts')
        writeFileSync(key, env.COMMON_REPO_SSH_KEY.trim() + '\n', { mode: 0o600 })
        writeFileSync(
          hosts,
          'github.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl\n',
        )
        env.GIT_SSH_COMMAND = `ssh -i ${quote(key)} -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=${quote(hosts)}`
        command = command.map((value) =>
          value === lock.repository
            ? value.replace('https://github.com/', 'git@github.com:')
            : value,
        )
      }
      if (env.COMMON_REPO_TOKEN && lock.repository.startsWith('https://github.com/')) {
        const index = Number(env.GIT_CONFIG_COUNT ?? 0)
        env.GIT_CONFIG_COUNT = String(index + 2)
        env[`GIT_CONFIG_KEY_${index}`] = 'http.https://github.com/.extraheader'
        env[`GIT_CONFIG_VALUE_${index}`] = ''
        env[`GIT_CONFIG_KEY_${index + 1}`] = 'http.https://github.com/.extraheader'
        env[`GIT_CONFIG_VALUE_${index + 1}`] =
          `AUTHORIZATION: basic ${Buffer.from(`x-access-token:${env.COMMON_REPO_TOKEN}`).toString('base64')}`
      }
      delete env.COMMON_REPO_TOKEN
      delete env.COMMON_REPO_SSH_KEY
      return execFileSync('git', ['-C', target, ...command], {
        encoding: 'utf8',
        env,
        stdio: ['ignore', 'pipe', 'pipe'],
      }).trim()
    } finally {
      if (credentials) rmSync(credentials, { recursive: true, force: true })
    }
  }
  if (!existsSync(join(target, '.git'))) git('init', '--quiet')
  let head = ''
  try {
    head = git('rev-parse', '--verify', 'HEAD')
  } catch {
    /* An empty checkout has no HEAD. */
  }
  if (head !== lock.revision) {
    if (head)
      throw new Error(`Unexpected revision in ${target}; inspect this cache before retrying`)
    git('fetch', '--quiet', '--depth=1', '--', lock.repository, lock.revision)
    git('checkout', '--quiet', '--detach', lock.revision)
  }
  if (
    git('rev-parse', 'HEAD') !== lock.revision ||
    git('status', '--porcelain', '--untracked-files=normal')
  ) {
    throw new Error(`Common checkout is not clean at ${lock.revision}`)
  }
  validate(target)
}
if (!existsSync(active) || realpathSync(active) !== realpathSync(target)) link(target, active)
process.stdout.write(`common: ${target}\n`)
