import { devPages as commonPages } from '@oxeu/center-common/tools/dev-pages.ts'

export const pageOptions = {
  backend: 'http://127.0.0.1:8080',
  home: '/projects',
  routes: /^\/(?:projects(?:\/[^?]*)?|users|settings|keys|login|docs(?:\/[^?]*)?)(?:\?|$)/u,
}

export function devPages() {
  return commonPages(pageOptions)
}
