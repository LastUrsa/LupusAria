import { useEffect, useRef, useState } from 'react'
import { GetAnnouncements, GetLogs, GetSettings, SaveAnnouncements, SaveSettings, StartBot, StopBot } from '../wailsjs/go/main/App'
import { main } from '../wailsjs/go/models'
import './App.css'

type Settings = main.ControlSettings
type Announcement = main.AnnouncementSettings
type Section = 'overview' | 'chat' | 'ai' | 'autoso' | 'ads' | 'announcements' | 'activity'

const sections: Array<{ id: Section; label: string }> = [
  { id: 'overview', label: 'Overview' },
  { id: 'chat', label: 'Chat' },
  { id: 'ai', label: 'AI' },
  { id: 'autoso', label: 'AutoSO' },
  { id: 'ads', label: 'Ads' },
  { id: 'announcements', label: 'Announcements' },
  { id: 'activity', label: 'Activity' }
]

const emptySettings: Settings = {
  running: false,
  status: 'Loading',
  error: '',
  channel: '',
  botUsername: '',
  configPath: '',
  twitchOAuthToken: '',
  twitchRefreshToken: '',
  twitchClientId: '',
  twitchClientSecret: '',
  twitchAdsOAuthToken: '',
  twitchAdsRefreshToken: '',
  hasTwitchOAuthToken: false,
  hasTwitchRefreshToken: false,
  hasTwitchClientId: false,
  hasTwitchClientSecret: false,
  hasTwitchAdsOAuthToken: false,
  hasTwitchAdsRefreshToken: false,
  aiProvider: 'mock',
  aiApiKey: '',
  geminiApiKey: '',
  aiModel: 'gpt-4.1-mini',
  geminiModel: 'gemini-3.1-flash-lite',
  maxRequestsPerHour: 30,
  dailyBudgetUsd: 0.5,
  monthlyBudgetUsd: 5,
  hasAiApiKey: false,
  hasGeminiApiKey: false,
  enableMentions: true,
  enableAsk: true,
  enableLurk: true,
  enableCommands: true,
  enableReset: true,
  globalCooldownSeconds: 6,
  userCooldownSeconds: 20,
  maxContextMessages: 30,
  autosoEnabled: true,
  recentStreamerMinWatch: 15,
  recentStreamerDays: 14,
  recentStreamerPageSize: 5,
  recentStreamerDelay: 2,
  adAlertsEnabled: false,
  adWarningMinutes: 5,
  adPollSeconds: 30,
  adWarningMessage: 'Heads up: ads are scheduled in about %s.',
  adStartMessage: 'Ad break starting now. Good moment to stretch, hydrate, and rest your eyes.',
  adEndMessage: 'Welcome back. Ads should be done now.',
  announcementsEnabled: false,
  announcementPollSeconds: 30
}

export default function App() {
  const [settings, setSettings] = useState<Settings>(emptySettings)
  const [logs, setLogs] = useState<string[]>([])
  const [announcements, setAnnouncements] = useState<Announcement[]>([])
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const [section, setSection] = useState<Section>('overview')
  const [dirty, setDirty] = useState(false)
  const dirtyRef = useRef(false)

  async function refresh(replaceSettings = false) {
    try {
      const next = await GetSettings()
      if (replaceSettings || !dirtyRef.current) {
        setSettings(next)
        setAnnouncements((await GetAnnouncements()) ?? [])
      } else {
        setSettings((current) => ({
          ...current,
          running: next.running,
          status: next.status,
          error: next.error
        }))
      }
      setLogs((await GetLogs()) ?? [])
    } catch (error) {
      setNotice(error instanceof Error ? error.message : String(error))
    }
  }

  useEffect(() => {
    refresh(true)
    const timer = window.setInterval(() => refresh(false), 3000)
    return () => window.clearInterval(timer)
  }, [])

  async function save() {
    setBusy(true)
    try {
      await SaveSettings(settings)
      await SaveAnnouncements(announcements)
      dirtyRef.current = false
      setDirty(false)
      setNotice('Settings saved. Restart the bot to apply runtime changes.')
      await refresh(true)
    } catch (error) {
      setNotice(error instanceof Error ? error.message : String(error))
    } finally {
      setBusy(false)
    }
  }

  async function start() {
    setBusy(true)
    try {
      await StartBot()
      setNotice('Bot starting.')
      await refresh(false)
    } catch (error) {
      setNotice(error instanceof Error ? error.message : String(error))
    } finally {
      setBusy(false)
    }
  }

  async function stop() {
    setBusy(true)
    try {
      await StopBot()
      setNotice('Bot stopping.')
      await refresh(false)
    } catch (error) {
      setNotice(error instanceof Error ? error.message : String(error))
    } finally {
      setBusy(false)
    }
  }

  const update = <K extends keyof Settings>(key: K, value: Settings[K]) => {
    dirtyRef.current = true
    setDirty(true)
    setSettings((current) => ({ ...current, [key]: value }))
  }

  const updateAnnouncement = <K extends keyof Announcement>(index: number, key: K, value: Announcement[K]) => {
    dirtyRef.current = true
    setDirty(true)
    setAnnouncements((current) => current.map((item, itemIndex) => (itemIndex === index ? { ...item, [key]: value } : item)))
  }

  const addAnnouncement = (kind: 'command' | 'timer') => {
    dirtyRef.current = true
    setDirty(true)
    const nextNumber = announcements.length + 1
    setAnnouncements((current) => [
      ...current,
      {
        id: `${kind}-${nextNumber}`,
        enabled: true,
        kind,
        command: kind === 'command' ? `!${kind}${nextNumber}` : '',
        afterMinutes: kind === 'timer' ? 30 : 0,
        repeatMinutes: kind === 'timer' ? 0 : 0,
        message: ''
      }
    ])
  }

  const removeAnnouncement = (index: number) => {
    dirtyRef.current = true
    setDirty(true)
    setAnnouncements((current) => current.filter((_, itemIndex) => itemIndex !== index))
  }

  return (
    <main className="app-shell">
      <aside className="sidebar">
        <div className="brand-block">
          <span className="suite-eyebrow">Starsong Tools</span>
          <h1>LupusAria</h1>
        </div>
        <nav className="section-nav" aria-label="Settings sections">
          {sections.map((item) => (
            <button
              key={item.id}
              className={section === item.id ? 'active' : ''}
              onClick={() => setSection(item.id)}
              type="button"
            >
              {item.label}
            </button>
          ))}
        </nav>
      </aside>

      <div className="workspace">
        <header className="topbar">
          <div>
            <h2>{sections.find((item) => item.id === section)?.label}</h2>
            <p>Local Twitch bot controls for chat replies, AutoSO, announcements, and ad alerts.</p>
          </div>
          <div className="runtime-panel">
            <span className={`status-pill ${settings.running ? 'running' : 'stopped'}`}>{settings.status}</span>
            <div className="runtime-actions">
              <button onClick={start} disabled={busy || settings.running}>Start</button>
              <button className="secondary" onClick={stop} disabled={busy || !settings.running}>Stop</button>
            </div>
          </div>
        </header>

        {notice && <div className="notice">{notice}</div>}
        {settings.error && <div className="notice error">{settings.error}</div>}

        <section className="panel">
          {section === 'overview' && (
            <div className="grid">
              <Card title="Twitch">
                <TextField label="Channel" value={settings.channel} onChange={(value) => update('channel', value)} />
                <TextField label="Bot username" value={settings.botUsername} onChange={(value) => update('botUsername', value)} />
                <ReadOnlyField label="Config path" value={settings.configPath} />
              </Card>
              <Card title="Runtime">
                <StatusRow label="Bot" value={settings.status} tone={settings.running ? 'good' : 'muted'} />
                <StatusRow label="AI provider" value={settings.aiProvider} />
                <StatusRow label="AutoSO" value={settings.autosoEnabled ? 'Enabled' : 'Disabled'} tone={settings.autosoEnabled ? 'good' : 'muted'} />
                <StatusRow label="Ad alerts" value={settings.adAlertsEnabled ? 'Enabled' : 'Disabled'} tone={settings.adAlertsEnabled ? 'good' : 'muted'} />
                <StatusRow label="Announcements" value={settings.announcementsEnabled ? 'Enabled' : 'Disabled'} tone={settings.announcementsEnabled ? 'good' : 'muted'} />
              </Card>
              <Card title="Twitch credentials" wide>
                <div className="info-callout">
                  <strong>Saved secrets are hidden.</strong>
                  <span>Leave a field blank to keep the saved value. Type a new value only when replacing it.</span>
                </div>
                <div className="split">
                  <SecretField label="Client ID" saved={settings.hasTwitchClientId} value={settings.twitchClientId} onChange={(value) => update('twitchClientId', value)} />
                  <SecretField label="Client secret" saved={settings.hasTwitchClientSecret} value={settings.twitchClientSecret} onChange={(value) => update('twitchClientSecret', value)} />
                </div>
                <div className="split">
                  <SecretField label="Bot OAuth token" saved={settings.hasTwitchOAuthToken} value={settings.twitchOAuthToken} onChange={(value) => update('twitchOAuthToken', value)} />
                  <SecretField label="Bot refresh token" saved={settings.hasTwitchRefreshToken} value={settings.twitchRefreshToken} onChange={(value) => update('twitchRefreshToken', value)} />
                </div>
                <div className="split">
                  <SecretField label="Ads OAuth token" saved={settings.hasTwitchAdsOAuthToken} value={settings.twitchAdsOAuthToken} onChange={(value) => update('twitchAdsOAuthToken', value)} />
                  <SecretField label="Ads refresh token" saved={settings.hasTwitchAdsRefreshToken} value={settings.twitchAdsRefreshToken} onChange={(value) => update('twitchAdsRefreshToken', value)} />
                </div>
              </Card>
            </div>
          )}

          {section === 'chat' && (
            <Card title="Chat abilities">
              <div className="toggle-grid">
                <Toggle label="Respond to mentions" checked={settings.enableMentions} onChange={(value) => update('enableMentions', value)} />
                <Toggle label="Enable !ask" checked={settings.enableAsk} onChange={(value) => update('enableAsk', value)} />
                <Toggle label="Enable !lurk" checked={settings.enableLurk} onChange={(value) => update('enableLurk', value)} />
                <Toggle label="Enable !commands" checked={settings.enableCommands} onChange={(value) => update('enableCommands', value)} />
                <Toggle label="Enable !reset" checked={settings.enableReset} onChange={(value) => update('enableReset', value)} />
              </div>
              <div className="split">
                <NumberField label="Global cooldown seconds" value={settings.globalCooldownSeconds} onChange={(value) => update('globalCooldownSeconds', value)} />
                <NumberField label="User cooldown seconds" value={settings.userCooldownSeconds} onChange={(value) => update('userCooldownSeconds', value)} />
              </div>
            </Card>
          )}

          {section === 'ai' && (
            <Card title="AI and cost rails">
              <label className="field">
                <span>Provider</span>
                <select value={settings.aiProvider} onChange={(event) => update('aiProvider', event.target.value)}>
                  <option value="mock">Mock</option>
                  <option value="gemini">Gemini</option>
                  <option value="openai-compatible">OpenAI-compatible</option>
                </select>
              </label>
              <TextField label="Gemini model" value={settings.geminiModel} onChange={(value) => update('geminiModel', value)} />
              <div className="split">
                <SecretField label="Gemini API key" saved={settings.hasGeminiApiKey} value={settings.geminiApiKey} onChange={(value) => update('geminiApiKey', value)} />
                <SecretField label="OpenAI-compatible API key" saved={settings.hasAiApiKey} value={settings.aiApiKey} onChange={(value) => update('aiApiKey', value)} />
              </div>
              <div className="split">
                <NumberField label="Requests per hour" value={settings.maxRequestsPerHour} onChange={(value) => update('maxRequestsPerHour', value)} />
                <NumberField label="Max context messages" value={settings.maxContextMessages} onChange={(value) => update('maxContextMessages', value)} />
              </div>
              <div className="split">
                <NumberField label="Daily budget" value={settings.dailyBudgetUsd} onChange={(value) => update('dailyBudgetUsd', value)} />
                <NumberField label="Monthly budget" value={settings.monthlyBudgetUsd} onChange={(value) => update('monthlyBudgetUsd', value)} />
              </div>
            </Card>
          )}

          {section === 'autoso' && (
            <Card title="AutoSO">
              <Toggle label="Enable AutoSO" checked={settings.autosoEnabled} onChange={(value) => update('autosoEnabled', value)} />
              <div className="split">
                <NumberField label="Minimum watch minutes" value={settings.recentStreamerMinWatch} onChange={(value) => update('recentStreamerMinWatch', value)} />
                <NumberField label="Recent stream days" value={settings.recentStreamerDays} onChange={(value) => update('recentStreamerDays', value)} />
              </div>
              <div className="split">
                <NumberField label="Page size" value={settings.recentStreamerPageSize} onChange={(value) => update('recentStreamerPageSize', value)} />
                <NumberField label="Shoutout delay seconds" value={settings.recentStreamerDelay} onChange={(value) => update('recentStreamerDelay', value)} />
              </div>
            </Card>
          )}

          {section === 'ads' && (
            <Card title="Ad alerts">
              <div className="info-callout">
                <strong>AI-powered alerts are the default.</strong>
                <span>These messages are fallbacks used when the AI provider is unavailable or the bot's AI limits are active.</span>
              </div>
              <Toggle label="Enable ad alerts" checked={settings.adAlertsEnabled} onChange={(value) => update('adAlertsEnabled', value)} />
              <div className="split">
                <NumberField label="Warning lead minutes" value={settings.adWarningMinutes} onChange={(value) => update('adWarningMinutes', value)} />
                <NumberField label="Poll seconds" value={settings.adPollSeconds} onChange={(value) => update('adPollSeconds', value)} />
              </div>
              <TextArea label="Warning fallback message" value={settings.adWarningMessage} onChange={(value) => update('adWarningMessage', value)} />
              <TextArea label="Start fallback message" value={settings.adStartMessage} onChange={(value) => update('adStartMessage', value)} />
              <TextArea label="End fallback message" value={settings.adEndMessage} onChange={(value) => update('adEndMessage', value)} />
            </Card>
          )}

          {section === 'announcements' && (
            <Card title="Announcements" wide>
              <div className="info-callout">
                <strong>Static messages, no AI cost.</strong>
                <span>Command announcements are broadcaster-only. Timer announcements use Twitch stream start time and repeat until stream end when an interval is set.</span>
              </div>
              <div className="split">
                <Toggle label="Enable announcements" checked={settings.announcementsEnabled} onChange={(value) => update('announcementsEnabled', value)} />
                <NumberField label="Timer poll seconds" value={settings.announcementPollSeconds} onChange={(value) => update('announcementPollSeconds', value)} />
              </div>
              <div className="announcement-actions">
                <button type="button" onClick={() => addAnnouncement('command')}>Add command</button>
                <button className="secondary" type="button" onClick={() => addAnnouncement('timer')}>Add timer</button>
              </div>
              <div className="announcement-list">
                {announcements.length === 0 ? (
                  <p className="muted">No announcements configured.</p>
                ) : announcements.map((item, index) => (
                  <div className="announcement-row" key={`${item.id}-${index}`}>
                    <div className="announcement-row-header">
                      <Toggle label="Enabled" checked={item.enabled} onChange={(value) => updateAnnouncement(index, 'enabled', value)} />
                      <label className="field compact-field">
                        <span>Type</span>
                        <select value={item.kind} onChange={(event) => updateAnnouncement(index, 'kind', event.target.value)}>
                          <option value="command">Command</option>
                          <option value="timer">Timer</option>
                        </select>
                      </label>
                      <button className="danger" type="button" onClick={() => removeAnnouncement(index)}>Remove</button>
                    </div>
                    <div className="split">
                      <TextField label="ID" value={item.id} onChange={(value) => updateAnnouncement(index, 'id', value)} />
                      {item.kind === 'timer' ? (
                        <>
                          <NumberField label="First send minute" value={item.afterMinutes} onChange={(value) => updateAnnouncement(index, 'afterMinutes', value)} />
                          <NumberField label="Repeat interval minutes" value={item.repeatMinutes} onChange={(value) => updateAnnouncement(index, 'repeatMinutes', value)} />
                        </>
                      ) : (
                        <TextField label="Command" value={item.command} onChange={(value) => updateAnnouncement(index, 'command', value)} />
                      )}
                    </div>
                    <TextArea label="Message" value={item.message} onChange={(value) => updateAnnouncement(index, 'message', value)} />
                  </div>
                ))}
              </div>
            </Card>
          )}

          {section === 'activity' && (
            <Card title="Activity">
              <div className="log-view">
                {logs.length === 0 ? <p className="muted">No activity yet.</p> : logs.slice(-18).map((line) => <div key={line}>{line}</div>)}
              </div>
            </Card>
          )}
        </section>

        <footer className="actions">
          <button onClick={save} disabled={busy}>Save settings</button>
          <button className="secondary" onClick={() => refresh(!dirty)} disabled={busy}>Refresh</button>
        </footer>
      </div>
    </main>
  )
}

function Card({ title, children, wide = false }: { title: string; children: React.ReactNode; wide?: boolean }) {
  return (
    <section className={`card ${wide ? 'wide' : ''}`}>
      <h2>{title}</h2>
      <div className="card-body">{children}</div>
    </section>
  )
}

function TextField({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <label className="field">
      <span>{label}</span>
      <input value={value} onChange={(event) => onChange(event.target.value)} />
    </label>
  )
}

function ReadOnlyField({ label, value }: { label: string; value: string }) {
  return (
    <label className="field">
      <span>{label}</span>
      <input value={value} readOnly />
    </label>
  )
}

function SecretField({ label, saved, value, onChange }: { label: string; saved: boolean; value: string; onChange: (value: string) => void }) {
  return (
    <label className="field">
      <span>{label}{saved ? ' saved' : ''}</span>
      <input type="password" value={value} placeholder={saved ? 'Saved' : ''} onChange={(event) => onChange(event.target.value)} />
    </label>
  )
}

function TextArea({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <label className="field">
      <span>{label}</span>
      <textarea value={value} onChange={(event) => onChange(event.target.value)} />
    </label>
  )
}

function NumberField({ label, value, onChange }: { label: string; value: number; onChange: (value: number) => void }) {
  return (
    <label className="field">
      <span>{label}</span>
      <input type="number" value={value} onChange={(event) => onChange(Number(event.target.value))} />
    </label>
  )
}

function Toggle({ label, checked, onChange }: { label: string; checked: boolean; onChange: (value: boolean) => void }) {
  return (
    <label className="toggle">
      <input type="checkbox" checked={checked} onChange={(event) => onChange(event.target.checked)} />
      <span>{label}</span>
    </label>
  )
}

function StatusRow({ label, value, tone = 'normal' }: { label: string; value: string; tone?: 'normal' | 'good' | 'muted' }) {
  return (
    <div className="status-row">
      <span>{label}</span>
      <strong className={tone}>{value}</strong>
    </div>
  )
}
