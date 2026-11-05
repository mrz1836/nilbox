import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { App } from './App'
import { brand } from './generated/brand'

describe('App', () => {
  it('shows the product name and tagline', () => {
    render(<App />)

    expect(screen.getByRole('heading', { level: 1, name: brand.name })).toBeInTheDocument()
    expect(screen.getByText(brand.tagline)).toBeInTheDocument()
  })
})
