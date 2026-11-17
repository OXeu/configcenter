import { createPageApplication } from '@oxeu/center-common/ui/application.ts'
import { loadPageData } from '@oxeu/center-common/ui/page-data.ts'
import { parsePageData } from './data.ts'
import { createPage } from './page.ts'
import { pageRoutes } from './routes.ts'
import App from './App.vue'
import '../theme.css'
import 'virtual:remixicon.css'

const source = document.querySelector('#page-data')
if (!source?.textContent) throw new Error('Missing page data')
const data = parsePageData(JSON.parse(source.textContent))
const { report } = createPage(data)
const { app, router } = createPageApplication({
  routes: pageRoutes(App),
  initial: { data, title: document.title },
  async load(path, signal) {
    const snapshot = await loadPageData(path, signal)
    return { ...snapshot, data: { ...parsePageData(snapshot.data), nonce: data.nonce } }
  },
  report,
  setTitle: (title) => {
    document.title = title
  },
})
void router
  .isReady()
  .then(() => app.mount('#app'))
  .catch(report)
