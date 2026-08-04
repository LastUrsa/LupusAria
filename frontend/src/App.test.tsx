import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { MediaActionsPanel, NavIcon } from './App'

const panelDefaults = {
  rewards: [],
  busy: false,
  onAdd: vi.fn(),
  onSelect: vi.fn(),
  onUpdate: vi.fn(),
  onRemove: vi.fn(),
  onLoadRewards: vi.fn(),
  onImportAssets: vi.fn(),
  onUpdateAssets: vi.fn(),
  onPreview: vi.fn(),
  overlayUrl: 'http://127.0.0.1:47831/'
}

describe('collapsed navigation icons', () => {
  it.each([
    'overview',
    'setup',
    'aiBudget',
    'features',
    'mediaActions',
    'knowledge'
  ] as const)('renders a recognizable SVG for %s', (section) => {
    const { container } = render(<NavIcon section={section} />)
    const icon = container.querySelector('svg.nav-icon')

    expect(icon).toBeInTheDocument()
    expect(icon).toHaveAttribute('aria-hidden', 'true')
    expect(icon?.childElementCount).toBeGreaterThan(0)
  })
})

describe('Media Actions responsive toolbar', () => {
  it('keeps New action available in the empty state', () => {
    const onAdd = vi.fn()

    render(
      <MediaActionsPanel
        {...panelDefaults}
        actions={[]}
        selectedAction={null}
        onAdd={onAdd}
      />
    )

    expect(screen.getByRole('combobox', { name: 'Action' })).toBeDisabled()
    expect(screen.getByRole('option', { name: 'No media actions yet' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'New action' }))
    expect(onAdd).toHaveBeenCalledOnce()
  })

  it('shows action details and changes the selected action', () => {
    const onSelect = vi.fn()
    const actions = [
      {
        id: 'first',
        name: 'First action',
        enabled: true,
        trigger: 'channel_point_redeem',
        rewardId: '',
        rewardTitle: 'First reward',
        media: [],
        sounds: [],
        duration: 5,
        position: 'center',
        scale: 100,
        animation: 'fade-in-out'
      },
      {
        id: 'second',
        name: 'Second action',
        enabled: true,
        trigger: 'channel_point_redeem',
        rewardId: '',
        rewardTitle: 'Second reward',
        media: [],
        sounds: [],
        duration: 9,
        position: 'center',
        scale: 100,
        animation: 'fade-out'
      }
    ]

    render(
      <MediaActionsPanel
        {...panelDefaults}
        actions={actions}
        selectedAction={actions[0]}
        onSelect={onSelect}
      />
    )

    expect(screen.getByRole('heading', { name: 'First action' })).toBeInTheDocument()
    fireEvent.change(screen.getByRole('combobox', { name: 'Action' }), { target: { value: 'second' } })
    expect(onSelect).toHaveBeenCalledWith('second')
  })

  it('edits caption content and typography', () => {
    const onUpdate = vi.fn()
    const action = {
      id: 'captioned',
      name: 'Captioned action',
      enabled: true,
      trigger: 'channel_point_redeem',
      rewardId: '',
      rewardTitle: 'Caption reward',
      media: [],
      sounds: [],
      duration: 5,
      position: 'center',
      scale: 100,
      animation: 'fade-in-out',
      text: 'Hello chat',
      textFont: 'Verdana',
      textSize: 36,
      textBold: false,
      textItalic: false,
      textUnderline: false,
      textColor: '#ff00aa'
    }

    render(
      <MediaActionsPanel
        {...panelDefaults}
        actions={[action]}
        selectedAction={action}
        onUpdate={onUpdate}
      />
    )

    expect(screen.getByText('Hello chat', { selector: '.caption-preview' })).toHaveStyle({
      fontFamily: 'Verdana',
      fontSize: '36px',
      color: '#ff00aa'
    })

    fireEvent.change(screen.getByRole('textbox', { name: 'Text under image' }), { target: { value: 'Updated caption' } })
    fireEvent.change(screen.getByRole('textbox', { name: 'Font' }), { target: { value: 'Georgia' } })
    fireEvent.change(screen.getByRole('spinbutton', { name: 'Size (px)' }), { target: { value: '48' } })
    fireEvent.click(screen.getByRole('checkbox', { name: 'Bold' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Italic' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Underline' }))

    expect(onUpdate).toHaveBeenCalledWith('captioned', 'text', 'Updated caption')
    expect(onUpdate).toHaveBeenCalledWith('captioned', 'textFont', 'Georgia')
    expect(onUpdate).toHaveBeenCalledWith('captioned', 'textSize', 48)
    expect(onUpdate).toHaveBeenCalledWith('captioned', 'textBold', true)
    expect(onUpdate).toHaveBeenCalledWith('captioned', 'textItalic', true)
    expect(onUpdate).toHaveBeenCalledWith('captioned', 'textUnderline', true)
  })
})
