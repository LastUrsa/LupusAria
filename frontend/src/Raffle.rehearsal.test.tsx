import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import App, { emptySettings } from './App'
import recordedTrace from './test/raffle-rehearsal.json'

// CI uses a trace freshly emitted by the real Go/Twitch/SQLite rehearsal.
// Standalone frontend runs replay the checked-in trace from that same test.
declare const __RAFFLE_REHEARSAL_TRACE__: typeof recordedTrace | null
const trace = __RAFFLE_REHEARSAL_TRACE__ ?? recordedTrace

afterEach(() => {
  cleanup()
  Reflect.deleteProperty(window, 'go')
  Reflect.deleteProperty(window, 'runtime')
  vi.useRealTimers()
})

function installBridge() {
  const listeners = new Map<string, (...args: any[]) => void>()
  let current = trace[0].snapshot
  let unavailable = false
  const getRaffle = vi.fn(async () => {
    if (unavailable) throw new Error('Simulated backend disconnect')
    return current
  })
  Object.defineProperty(window, 'go', { configurable: true, value: { main: { App: {
    GetSettings: async () => ({ ...emptySettings, running: true, status: 'Running', channel: 'rehearsal', botUsername: 'bot' }),
    GetAnnouncements: async () => [], GetMediaActions: async () => [],
    GetKnowledge: async () => ({ path: '', exists: false, content: '' }),
    GetMediaOverlayURL: async () => '', GetLogs: async () => [],
    GetWrapUp: async () => ({ status: 'none', formatted: '' }), GetRaffle: getRaffle
  } } } })
  Object.defineProperty(window, 'runtime', { configurable: true, value: {
    EventsOnMultiple: (name: string, callback: (...args: any[]) => void) => { listeners.set(name, callback); return () => listeners.delete(name) },
    EventsOff: (name: string) => listeners.delete(name)
  } })
  return {
    getRaffle,
    snapshot(label: string) { const step = trace.find(step => step.label === label); if (!step) throw new Error(`Missing Go trace step: ${label}`); return step.snapshot },
    publish(snapshot: typeof current) { current = snapshot; listeners.get('raffle-updated')?.(snapshot) },
    setSnapshot(snapshot: typeof current) { current = snapshot },
    disconnect() { unavailable = true }, reconnect() { unavailable = false }
  }
}

it('replays real backend lifecycle snapshots through the complete app and Wails event bridge', async () => {
  const bridge = installBridge()
  render(<App />)
  await waitFor(() => expect(bridge.getRaffle).toHaveBeenCalled())
  fireEvent.click(screen.getByRole('button', { name: 'Features' }))
  const title = screen.getByText('Raffle')
  const panel = title.closest('details')!
  fireEvent.click(title.closest('summary')!)
  for (const step of trace) {
    await act(async () => bridge.publish(step.snapshot))
    expect(within(panel).getByText(`Total Entries: ${step.snapshot.entries.length}`)).toBeInTheDocument()
    expect(within(panel).getByText(`Status: ${step.snapshot.status}`)).toBeInTheDocument()
    if (step.snapshot.winner) expect(within(panel).getByText(`#${step.snapshot.winner.number} — ${step.snapshot.winner.name}`)).toBeInTheDocument()
    else expect(within(panel).queryByText('Latest Winner')).not.toBeInTheDocument()
  }
  // A delayed event from A cannot restore old entries after B became current.
  await act(async () => bridge.publish(bridge.snapshot('winner')))
  expect(within(panel).queryByText('Viewer1')).not.toBeInTheDocument()
  expect(within(panel).getByText('ViewerB')).toBeInTheDocument()
})

it('fetches a complete snapshot on UI restart and reconnect, marking cached data unverified during failure', async () => {
  vi.useFakeTimers()
  const bridge = installBridge()
  bridge.setSnapshot(bridge.snapshot('five-entries'))
  const first = render(<App />)
  await act(async () => { await vi.advanceTimersByTimeAsync(0) })
  fireEvent.click(screen.getByRole('button', { name: 'Features' }))
  expect(screen.getByText('Total Entries: 5')).toBeInTheDocument()
  bridge.disconnect()
  await act(async () => { await vi.advanceTimersByTimeAsync(3000) })
  expect(screen.getByText('Status: RECOVERING — backend unavailable; showing saved display')).toBeInTheDocument()
  bridge.setSnapshot(bridge.snapshot('cleared')); bridge.reconnect()
  await act(async () => { await vi.advanceTimersByTimeAsync(3000) })
  expect(screen.getByText('Total Entries: 0')).toBeInTheDocument()
  expect(screen.queryByText('Viewer1')).not.toBeInTheDocument()
  first.unmount()
  bridge.setSnapshot(bridge.snapshot('re-entered'))
  render(<App />)
  await act(async () => { await vi.advanceTimersByTimeAsync(0) })
  fireEvent.click(screen.getByRole('button', { name: 'Features' }))
  expect(screen.getByText('Total Entries: 1')).toBeInTheDocument()
  expect(screen.getByText('Viewer1')).toBeInTheDocument()
})
