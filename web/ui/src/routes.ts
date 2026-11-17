import type { RouteRecordRaw } from 'vue-router'
import type { Component } from 'vue'

export function pageRoutes(component: Component): RouteRecordRaw[] {
  return [
    ...['/projects', '/projects/:project', '/users', '/settings', '/keys'].map((path) => ({
      path,
      component,
    })),
    { path: '/:pathMatch(.*)*', component, meta: { document: true } },
  ]
}
