import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { meOwner } from '@/test/fixtures'
import { mockApi, renderRoute } from '@/test/utils'

describe('чужой центр в адресе', () => {
  it('«Не найдено» с кнопкой «На главную», без молчаливого перехода', async () => {
    mockApi({ 'GET /api/v1/me': { body: meOwner } })
    const { router } = renderRoute('/c/00000000-0000-0000-0000-000000000099/students/s1')
    expect(await screen.findByText('Не найдено')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'На главную' })).toHaveAttribute('href', '/')
    expect(router.state.location.pathname).toBe('/c/00000000-0000-0000-0000-000000000099/students/s1')
  })
})
