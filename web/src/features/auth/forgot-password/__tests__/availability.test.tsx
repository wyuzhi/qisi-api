/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'

import { STATUS_QUERY_KEY } from '@/lib/status-query'

import { ForgotPassword } from '..'

function renderRecovery(emailRecoveryEnabled: boolean) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(STATUS_QUERY_KEY, {
    email_password_reset_enabled: emailRecoveryEnabled,
  })
  const root = createRootRoute({ component: Outlet })
  const router = createRouter({
    routeTree: root.addChildren([
      createRoute({
        getParentRoute: () => root,
        path: '/forgot-password',
        component: ForgotPassword,
      }),
      createRoute({
        getParentRoute: () => root,
        path: '/sign-in',
        component: () => <p>Sign in</p>,
      }),
      createRoute({
        getParentRoute: () => root,
        path: '/sign-up',
        component: () => <p>Sign up</p>,
      }),
    ]),
    history: createMemoryHistory({ initialEntries: ['/forgot-password'] }),
  })
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
})

it('explains unavailable email recovery and hides the form until configured', async () => {
  renderRecovery(false)
  expect(
    await screen.findByText(
      'Email password recovery is not available yet. If you cannot sign in, contact the site administrator for help.'
    )
  ).toBeVisible()
  expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Back to sign in' })).toHaveAttribute(
    'href',
    '/sign-in'
  )
})

it('keeps the existing recovery form when an email service is configured', async () => {
  renderRecovery(true)
  expect(await screen.findByRole('textbox')).toBeVisible()
  expect(
    screen.queryByText(
      'Email password recovery is not available yet. If you cannot sign in, contact the site administrator for help.'
    )
  ).not.toBeInTheDocument()
})
