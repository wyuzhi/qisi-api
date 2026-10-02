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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { STATUS_QUERY_KEY } from '@/lib/status-query'

import { SignUpForm } from '../components/sign-up-form'

function renderSignUp(minimum: number, emailRecovery: boolean) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  queryClient.setQueryData(STATUS_QUERY_KEY, {
    registration_password_min_length: minimum,
    email_password_reset_enabled: emailRecovery,
    register_enabled: true,
    password_register_enabled: true,
    email_verification: false,
    oauth_register_enabled: false,
  })
  const root = createRootRoute({ component: Outlet })
  const router = createRouter({
    routeTree: root.addChildren([
      createRoute({
        getParentRoute: () => root,
        path: '/sign-up',
        component: SignUpForm,
      }),
      createRoute({
        getParentRoute: () => root,
        path: '/sign-in',
        component: () => <p>Sign-in destination</p>,
      }),
    ]),
    history: createMemoryHistory({ initialEntries: ['/sign-up'] }),
  })
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  return queryClient
}

beforeEach(() => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

it('enforces the server registration minimum before submitting and accepts a longer passphrase', async () => {
  const submit = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true } })
  const user = userEvent.setup()
  renderSignUp(15, false)
  await user.type(await screen.findByLabelText('Username'), 'creator')
  await user.type(
    screen.getByLabelText('Password', { exact: true }),
    'shortpass'
  )
  await user.type(
    screen.getByLabelText('Confirm password', { exact: true }),
    'shortpass'
  )
  await user.click(screen.getByRole('button', { name: 'Create account' }))
  await waitFor(() =>
    expect(screen.getByLabelText('Password', { exact: true })).toHaveAttribute(
      'aria-invalid',
      'true'
    )
  )
  expect(submit).not.toHaveBeenCalled()
  expect(
    screen.getByPlaceholderText('Enter password (15–128 characters)')
  ).toBeVisible()

  await user.clear(screen.getByLabelText('Password', { exact: true }))
  await user.type(
    screen.getByLabelText('Password', { exact: true }),
    'a creative account password'
  )
  await user.clear(screen.getByLabelText('Confirm password', { exact: true }))
  await user.type(
    screen.getByLabelText('Confirm password', { exact: true }),
    'a creative account password'
  )
  await user.click(screen.getByRole('button', { name: 'Create account' }))
  await screen.findByText('Sign-in destination')
  expect(submit).toHaveBeenCalledTimes(1)
})

it('explains username sign-in when email recovery is unavailable', async () => {
  renderSignUp(15, false)
  expect(
    await screen.findByText(
      'Sign in with your username. Keep your username and password safe; email password recovery is not available yet.'
    )
  ).toBeVisible()
  expect(
    screen.queryByRole('textbox', { name: 'Email (required for verification)' })
  ).not.toBeInTheDocument()
})
