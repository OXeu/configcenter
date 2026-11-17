import { compressDirectory } from '@oxeu/center-common/tools/compress-assets.ts'

await Promise.all(['.tmp/ui', '.tmp/docs'].map(compressDirectory))
