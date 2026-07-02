import { useEffect, useRef, useState } from 'react'
import { EventsOff, EventsOn } from '../wailsjs/runtime/runtime'
import { CheckTwitchPermissions, GetAnnouncements, GetChannelPointRewards, GetKnowledge, GetLogs, GetMediaActions, GetMediaAssetDataURL, GetMediaOverlayURL, GetSettings, ImportMediaActionAssets, PreviewMediaAction, ResetKnowledgeTemplate, SaveAnnouncements, SaveKnowledge, SaveMediaActions, SaveSettings, StartBot, StopBot } from '../wailsjs/go/main/App'
import { main } from '../wailsjs/go/models'
import lupusAriaIcon from './assets/images/LupusAriaIcon.png'
import './App.css'

type Settings = main.ControlSettings
type Announcement = main.AnnouncementSettings
type Knowledge = main.KnowledgeSettings
type TwitchPermissionCheck = main.TwitchPermissionCheck
type MediaAsset = {
  id: string
  filename: string
  path: string
  durationMs?: number
  mediaPlaybackMode?: string
  excludeFromGifRotation?: boolean
}
type MediaAction = {
  id: string
  name: string
  enabled: boolean
  trigger: string
  rewardId: string
  rewardTitle: string
  media: MediaAsset[]
  sounds: MediaAsset[]
  duration: number
  position: string
  scale: number
  animation: string
}
type ChannelPointReward = {
  id: string
  title: string
  prompt: string
  enabled: boolean
}
type MediaActionPlayback = {
  actionId: string
  name: string
  media?: MediaAsset
  sound?: MediaAsset
  mediaDataUrl: string
  soundDataUrl: string
  duration: number
  position: string
  scale: number
  animation: string
  mediaDurationMs?: number
  mediaFrameDataUrls?: string[]
  mediaFrameDelaysMs?: number[]
  mediaPlaybackMode?: string
  mediaClips?: MediaPlaybackClip[]
}
type MediaPlaybackClip = {
  media: MediaAsset
  mediaDataUrl: string
  mediaDurationMs?: number
  mediaFrameDataUrls?: string[]
  mediaFrameDelaysMs?: number[]
}
type Section = 'overview' | 'setup' | 'aiBudget' | 'features' | 'mediaActions' | 'knowledge'
type AnnouncementKind = 'command' | 'timer'
type IndexedAnnouncement = { item: Announcement; index: number }
type AnnouncementUpdate = <K extends keyof Announcement>(index: number, key: K, value: Announcement[K]) => void

const sections: Array<{ id: Section; label: string }> = [
  { id: 'overview', label: 'Overview' },
  { id: 'setup', label: 'Setup' },
  { id: 'aiBudget', label: 'AI & Budget' },
  { id: 'features', label: 'Features' },
  { id: 'mediaActions', label: 'Media Actions' },
  { id: 'knowledge', label: 'Knowledge' }
]

const aiProviderOptions = [
  { value: 'mock', label: 'Mock' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'openai-compatible', label: 'OpenAI-compatible' }
]

const announcementKindOptions = [
  { value: 'command', label: 'Command' },
  { value: 'timer', label: 'Timer' }
]

const permissionOptions = [
  { value: 'everyone', label: 'Everyone' },
  { value: 'mods', label: 'Mods + broadcaster' },
  { value: 'broadcaster', label: 'Broadcaster only' }
]

const triggerOptions = [
  { value: 'channel_point_redeem', label: 'Channel Point Redeem' }
]

const positionOptions = [
  { value: 'center', label: 'Center' },
  { value: 'top-left', label: 'Top Left' },
  { value: 'top-right', label: 'Top Right' },
  { value: 'bottom-left', label: 'Bottom Left' },
  { value: 'bottom-right', label: 'Bottom Right' }
]

const animationOptions = [
  { value: 'none', label: 'None' },
  { value: 'fade-in', label: 'Fade In' },
  { value: 'fade-out', label: 'Fade Out' },
  { value: 'fade-in-out', label: 'Fade In + Fade Out' }
]

const mediaPlaybackModeOptions = [
  { value: 'normal', label: 'Normal' },
  { value: 'match_audio', label: 'Slow to Audio' },
  { value: 'loop', label: 'Loop' },
  { value: 'loop_next', label: 'Loop to Another GIF' }
]

const emptySettings: Settings = {
  running: false,
  status: 'Loading',
  error: '',
  channel: '',
  botUsername: '',
  configPath: '',
  streamerName: '',
  streamerPronouns: '',
  knowledgePath: '',
  knowledgeExists: false,
  twitchOAuthToken: '',
  twitchRefreshToken: '',
  twitchClientId: '',
  twitchClientSecret: '',
  twitchAdsClientId: '',
  twitchAdsClientSecret: '',
  twitchAdsOAuthToken: '',
  twitchAdsRefreshToken: '',
  hasTwitchOAuthToken: false,
  hasTwitchRefreshToken: false,
  hasTwitchClientId: false,
  hasTwitchClientSecret: false,
  hasTwitchAdsClientId: false,
  hasTwitchAdsClientSecret: false,
  hasTwitchAdsOAuthToken: false,
  hasTwitchAdsRefreshToken: false,
  aiProvider: 'mock',
  aiApiKey: '',
  geminiApiKey: '',
  aiModel: '',
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
  mentionPermission: 'everyone',
  askPermission: 'everyone',
  lurkPermission: 'everyone',
  gamePermission: 'everyone',
  commandsPermission: 'everyone',
  resetPermission: 'broadcaster',
  autosoPermission: 'mods',
  soRoulettePermission: 'mods',
  globalCooldownSeconds: 6,
  userCooldownSeconds: 20,
  maxContextMessages: 30,
  gameSnapshotCropEnabled: true,
  gameSnapshotCropX: 0.255,
  gameSnapshotCropY: 0.085,
  gameSnapshotCropWidth: 0.73,
  gameSnapshotCropHeight: 0.73,
  autosoEnabled: true,
  recentStreamerMinWatch: 15,
  recentStreamerDays: 14,
  recentStreamerPageSize: 5,
  recentStreamerDelay: 5,
  soRouletteStreamers: '',
  adAlertsEnabled: false,
  adWarningMinutes: 5,
  adPollSeconds: 30,
  adWarningMessage: 'Heads up: ads are scheduled in about %s.',
  adStartMessage: 'Ad break starting now. Good moment to stretch, hydrate, and rest your eyes.',
  adEndMessage: 'Welcome back. Ads should be done now.',
  announcementsEnabled: false,
  announcementPollSeconds: 30
}

const emptyKnowledge: Knowledge = {
  path: '',
  exists: false,
  content: ''
}

function createEmptyMediaAction(index: number): MediaAction {
  return {
    id: `media-action-${Date.now()}-${index}`,
    name: `Media Action ${index}`,
    enabled: true,
    trigger: 'channel_point_redeem',
    rewardId: '',
    rewardTitle: '',
    media: [],
    sounds: [],
    duration: 5,
    position: 'center',
    scale: 100,
    animation: 'fade-in-out'
  } as MediaAction
}

export default function App() {
  const [settings, setSettings] = useState<Settings>(emptySettings)
  const [knowledge, setKnowledge] = useState<Knowledge>(emptyKnowledge)
  const [logs, setLogs] = useState<string[]>([])
  const [announcements, setAnnouncements] = useState<Announcement[]>([])
  const [mediaActions, setMediaActions] = useState<MediaAction[]>([])
  const [selectedMediaActionId, setSelectedMediaActionId] = useState('')
  const [channelPointRewards, setChannelPointRewards] = useState<ChannelPointReward[]>([])
  const [activePlayback, setActivePlayback] = useState<MediaActionPlayback | null>(null)
  const [mediaOverlayUrl, setMediaOverlayUrl] = useState('')
  const [permissionCheck, setPermissionCheck] = useState<TwitchPermissionCheck | null>(null)
  const [notice, setNotice] = useState('')
  const [toast, setToast] = useState('')
  const [busy, setBusy] = useState(false)
  const [section, setSection] = useState<Section>('overview')
  const [dirty, setDirty] = useState(false)
  const dirtyRef = useRef(false)
  const toastTimerRef = useRef<number | null>(null)
  const playbackTimerRef = useRef<number | null>(null)

  async function refresh(replaceSettings = false) {
    try {
      const next = await GetSettings()
      if (replaceSettings || !dirtyRef.current) {
        setSettings(next)
        setAnnouncements((await GetAnnouncements()) ?? [])
        const nextMediaActions = (await GetMediaActions()) ?? []
        setMediaActions(nextMediaActions)
        setSelectedMediaActionId((current) => current || nextMediaActions[0]?.id || '')
        setKnowledge((await GetKnowledge()) ?? emptyKnowledge)
        setMediaOverlayUrl(await GetMediaOverlayURL())
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
    return () => {
      window.clearInterval(timer)
      if (toastTimerRef.current) {
        window.clearTimeout(toastTimerRef.current)
      }
      if (playbackTimerRef.current) {
        window.clearTimeout(playbackTimerRef.current)
      }
      EventsOff('media-action-playback')
    }
  }, [])

  useEffect(() => {
    EventsOn('media-action-playback', (playback: MediaActionPlayback) => {
      setActivePlayback(playback)
      if (playback.soundDataUrl) {
        const audio = new Audio(playback.soundDataUrl)
        audio.play().catch(() => undefined)
      }
      if (playbackTimerRef.current) {
        window.clearTimeout(playbackTimerRef.current)
      }
      playbackTimerRef.current = window.setTimeout(() => {
        setActivePlayback(null)
        playbackTimerRef.current = null
      }, Math.max(1, playback.duration || 5) * 1000)
    })
    return () => EventsOff('media-action-playback')
  }, [])

  function showToast(message: string) {
    setToast(message)
    if (toastTimerRef.current) {
      window.clearTimeout(toastTimerRef.current)
    }
    toastTimerRef.current = window.setTimeout(() => {
      setToast('')
      toastTimerRef.current = null
    }, 3200)
  }

  async function save() {
    setBusy(true)
    try {
      await SaveSettings(settings)
      await SaveAnnouncements(announcements)
      await SaveMediaActions(mediaActions as any)
      await SaveKnowledge(knowledge)
      dirtyRef.current = false
      setDirty(false)
      setNotice('Settings saved. Restart the bot to apply runtime changes.')
      showToast('Settings saved.')
      await refresh(true)
    } catch (error) {
      setNotice(error instanceof Error ? error.message : String(error))
    } finally {
      setBusy(false)
    }
  }

  async function reloadKnowledge() {
    setBusy(true)
    try {
      setKnowledge((await GetKnowledge()) ?? emptyKnowledge)
      setNotice('Knowledge reloaded.')
    } catch (error) {
      setNotice(error instanceof Error ? error.message : String(error))
    } finally {
      setBusy(false)
    }
  }

  async function resetKnowledge() {
    setBusy(true)
    try {
      setKnowledge((await ResetKnowledgeTemplate()) ?? emptyKnowledge)
      setNotice('Knowledge reset from template.')
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

  async function checkTwitchPermissions() {
    setBusy(true)
    try {
      const result = await CheckTwitchPermissions()
      setPermissionCheck(result)
      setNotice(`Twitch permissions check: ${result.overall}.`)
      await refresh(false)
    } catch (error) {
      setNotice(error instanceof Error ? error.message : String(error))
    } finally {
      setBusy(false)
    }
  }

  async function loadChannelPointRewards() {
    setBusy(true)
    try {
      const rewards = (await GetChannelPointRewards()) ?? []
      setChannelPointRewards(rewards)
      setNotice(`Loaded ${rewards.length} channel point rewards.`)
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

  const updateKnowledge = <K extends keyof Knowledge>(key: K, value: Knowledge[K]) => {
    dirtyRef.current = true
    setDirty(true)
    setKnowledge((current) => ({ ...current, [key]: value }))
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
        permission: kind === 'command' ? 'mods' : '',
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

  const addMediaAction = () => {
    dirtyRef.current = true
    setDirty(true)
    const action = createEmptyMediaAction(mediaActions.length + 1)
    setMediaActions((current) => [...current, action])
    setSelectedMediaActionId(action.id)
  }

  const updateMediaAction = <K extends keyof MediaAction>(id: string, key: K, value: MediaAction[K]) => {
    dirtyRef.current = true
    setDirty(true)
    setMediaActions((current) => current.map((item) => (item.id === id ? { ...item, [key]: value } : item)))
  }

  const removeMediaAction = (id: string) => {
    dirtyRef.current = true
    setDirty(true)
    setMediaActions((current) => {
      const next = current.filter((item) => item.id !== id)
      setSelectedMediaActionId(next[0]?.id || '')
      return next
    })
  }

  const importMediaActionAssets = async (action: MediaAction, kind: 'media' | 'sound') => {
    setBusy(true)
    try {
      const imported = (await ImportMediaActionAssets(action as any, kind)) ?? []
      if (imported.length > 0) {
        dirtyRef.current = true
        setDirty(true)
        setMediaActions((current) => current.map((item) => {
          if (item.id !== action.id) {
            return item
          }
          return kind === 'media'
            ? { ...item, media: [...(item.media ?? []), ...imported] }
            : { ...item, sounds: [...(item.sounds ?? []), ...imported] }
        }))
        showToast(`Imported ${imported.length} ${kind === 'media' ? 'media' : 'sound'} file${imported.length === 1 ? '' : 's'}.`)
      }
    } catch (error) {
      setNotice(error instanceof Error ? error.message : String(error))
    } finally {
      setBusy(false)
    }
  }

  const updateMediaActionAssets = (actionId: string, kind: 'media' | 'sound', assets: MediaAsset[]) => {
    dirtyRef.current = true
    setDirty(true)
    setMediaActions((current) => current.map((item) => {
      if (item.id !== actionId) {
        return item
      }
      return kind === 'media' ? { ...item, media: assets } : { ...item, sounds: assets }
    }))
  }

  const previewMediaAction = async (action: MediaAction) => {
    setBusy(true)
    try {
      await PreviewMediaAction(action as any)
    } catch (error) {
      setNotice(error instanceof Error ? error.message : String(error))
    } finally {
      setBusy(false)
    }
  }

  const commandAnnouncements = announcements
    .map((item, index) => ({ item, index }))
    .filter(({ item }) => item.kind === 'command')
  const timerAnnouncements = announcements
    .map((item, index) => ({ item, index }))
    .filter(({ item }) => item.kind === 'timer')
  const selectedMediaAction = mediaActions.find((item) => item.id === selectedMediaActionId) ?? mediaActions[0] ?? null
  const currentSection = sections.find((item) => item.id === section) ?? sections[0]
  const setupMissing = [
    settings.channel,
    settings.botUsername,
    settings.streamerName,
    settings.streamerPronouns
  ].filter((value) => !value.trim()).length
  const twitchCredentialReady = settings.hasTwitchOAuthToken || settings.hasTwitchRefreshToken || settings.twitchOAuthToken.trim() !== '' || settings.twitchRefreshToken.trim() !== ''
  const twitchAppReady = settings.hasTwitchClientId || settings.twitchClientId.trim() !== ''
  const setupState = setupMissing === 0 && twitchCredentialReady && twitchAppReady ? 'Ready' : 'Needs setup'
  const aiReady = settings.aiProvider === 'mock' || settings.hasGeminiApiKey || settings.hasAiApiKey || settings.geminiApiKey.trim() !== '' || settings.aiApiKey.trim() !== ''
  const chatRepliesEnabled = settings.enableMentions || settings.enableAsk || settings.enableLurk
  const canSaveSection = section !== 'overview'
  const topbarCopy: Record<Section, string> = {
    overview: 'Status, launch controls, and recent activity.',
    setup: 'Twitch account, streamer identity, and credentials.',
    aiBudget: 'Provider, models, keys, context, and cost rails.',
    features: 'Chat behavior, AutoSO, ad alerts, and announcements.',
    mediaActions: 'Random media and sounds for channel point redeems.',
    knowledge: 'Stable channel facts for AI replies.'
  }

  return (
    <main className="app-shell">
      <aside className="sidebar">
        <div className="brand-block">
          <img className="app-logo" src={lupusAriaIcon} alt="" aria-hidden="true" />
          <div>
            <h1>LupusAria</h1>
            <span className="suite-eyebrow">Starsong Tools</span>
          </div>
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
            <h2>{currentSection.label}</h2>
            <p>{topbarCopy[section]}</p>
          </div>
          <div className="runtime-panel">
            <span className={`runtime-state ${settings.running ? 'running' : 'stopped'}`}>
              <span aria-hidden="true" />
              {settings.status}
            </span>
            <div className="runtime-actions">
              <button onClick={start} disabled={busy || settings.running}>Start</button>
              <button className="secondary" onClick={stop} disabled={busy || !settings.running}>Stop</button>
            </div>
          </div>
        </header>

        {notice && <div className="notice">{notice}</div>}
        {settings.error && <div className="notice error">{settings.error}</div>}
        {toast && <div className="toast" role="status">{toast}</div>}
        <MediaActionOverlay playback={activePlayback} />

        <section className="panel">
          {section === 'overview' && (
            <div className="overview-stack">
              <section className="status-strip" aria-label="Runtime summary">
                <StatusChip label="Setup" value={setupState} tone={setupState === 'Ready' ? 'good' : 'warning'} />
                <StatusChip label="Channel" value={settings.channel || 'Not set'} tone={settings.channel ? 'normal' : 'warning'} />
                <StatusChip label="AI" value={aiReady ? settings.aiProvider : 'Needs key'} tone={aiReady ? 'good' : 'warning'} />
                <StatusChip label="Knowledge" value={settings.knowledgeExists ? 'Ready' : 'Missing'} tone={settings.knowledgeExists ? 'good' : 'warning'} />
                <StatusChip label="Chat" value={chatRepliesEnabled ? 'Enabled' : 'Disabled'} tone={chatRepliesEnabled ? 'good' : 'muted'} />
                <StatusChip label="AutoSO" value={settings.autosoEnabled ? 'Enabled' : 'Disabled'} tone={settings.autosoEnabled ? 'good' : 'muted'} />
                <StatusChip label="Ads" value={settings.adAlertsEnabled ? 'Enabled' : 'Disabled'} tone={settings.adAlertsEnabled ? 'good' : 'muted'} />
                <StatusChip label="Announcements" value={settings.announcementsEnabled ? `${announcements.length} set` : 'Disabled'} tone={settings.announcementsEnabled ? 'good' : 'muted'} />
                <StatusChip label="Media Actions" value={mediaActions.length > 0 ? `${mediaActions.length} set` : 'None'} tone={mediaActions.some((item) => item.enabled) ? 'good' : 'muted'} />
              </section>
              <Card title="Activity" wide>
                <div className="log-view full">
                  {logs.length === 0 ? <p className="muted">No activity yet.</p> : logs.slice(-28).map((line) => <div key={line}>{line}</div>)}
                </div>
              </Card>
            </div>
          )}

          {section === 'setup' && (
            <div className="grid">
              <Card title="Channel identity">
                <TextField label="Channel" value={settings.channel} onChange={(value) => update('channel', value)} />
                <TextField label="Bot username" value={settings.botUsername} onChange={(value) => update('botUsername', value)} />
                <TextField label="Streamer name" value={settings.streamerName} onChange={(value) => update('streamerName', value)} />
                <TextField label="Streamer pronouns" value={settings.streamerPronouns} onChange={(value) => update('streamerPronouns', value)} />
              </Card>
              <Card title="Saved credentials">
                <StatusRow label="Twitch app" value={settings.hasTwitchClientId ? 'Saved' : 'Missing'} tone={settings.hasTwitchClientId ? 'good' : 'muted'} />
                <StatusRow label="Bot token" value={settings.hasTwitchOAuthToken || settings.hasTwitchRefreshToken ? 'Saved' : 'Missing'} tone={settings.hasTwitchOAuthToken || settings.hasTwitchRefreshToken ? 'good' : 'muted'} />
                <StatusRow label="Ads token" value={settings.hasTwitchAdsOAuthToken || settings.hasTwitchAdsRefreshToken ? 'Saved' : 'Optional'} tone={settings.hasTwitchAdsOAuthToken || settings.hasTwitchAdsRefreshToken ? 'good' : 'muted'} />
                <div className="permission-check-header">
                  <div>
                    <strong>{permissionCheck ? `Permissions: ${permissionCheck.overall}` : 'Permissions'}</strong>
                    <span>{permissionCheck ? `Checked ${formatCheckedAt(permissionCheck.checkedAt)}` : 'Validate saved tokens and scopes.'}</span>
                  </div>
                  <button type="button" onClick={checkTwitchPermissions} disabled={busy}>Check permissions</button>
                </div>
                {permissionCheck && (
                  <div className="permission-results">
                    {permissionCheck.items.map((item) => (
                      <PermissionResult key={item.name} item={item} />
                    ))}
                  </div>
                )}
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
                <div className="split">
                  <SecretField label="Ads client ID" saved={settings.hasTwitchAdsClientId} value={settings.twitchAdsClientId} onChange={(value) => update('twitchAdsClientId', value)} />
                  <SecretField label="Ads client secret" saved={settings.hasTwitchAdsClientSecret} value={settings.twitchAdsClientSecret} onChange={(value) => update('twitchAdsClientSecret', value)} />
                </div>
              </Card>
            </div>
          )}

          {section === 'aiBudget' && (
            <div className="grid">
              <Card title="AI provider">
                <SelectField label="Provider" value={settings.aiProvider} options={aiProviderOptions} onChange={(value) => update('aiProvider', value)} />
                <TextField label="OpenAI-compatible model" value={settings.aiModel} onChange={(value) => update('aiModel', value)} />
                <TextField label="Gemini model" value={settings.geminiModel} onChange={(value) => update('geminiModel', value)} />
                <div className="split">
                  <SecretField label="Gemini API key" saved={settings.hasGeminiApiKey} value={settings.geminiApiKey} onChange={(value) => update('geminiApiKey', value)} />
                  <SecretField label="OpenAI-compatible API key" saved={settings.hasAiApiKey} value={settings.aiApiKey} onChange={(value) => update('aiApiKey', value)} />
                </div>
              </Card>
              <Card title="Budget and context">
                <NumberField label="Requests per hour" value={settings.maxRequestsPerHour} onChange={(value) => update('maxRequestsPerHour', value)} />
                <NumberField label="Max context messages" value={settings.maxContextMessages} onChange={(value) => update('maxContextMessages', value)} />
                <div className="split">
                  <NumberField label="Daily budget" value={settings.dailyBudgetUsd} onChange={(value) => update('dailyBudgetUsd', value)} />
                  <NumberField label="Monthly budget" value={settings.monthlyBudgetUsd} onChange={(value) => update('monthlyBudgetUsd', value)} />
                </div>
              </Card>
            </div>
          )}

          {section === 'knowledge' && (
            <Card title="Streamer knowledge" wide>
              <div className="info-callout">
                <strong>Stable channel facts only.</strong>
                <span>Use this space for streamer identity, recurring chat references, projects, links, and boundaries. Avoid secrets and fast-changing details.</span>
              </div>
              <TextArea label="Knowledge markdown" value={knowledge.content} onChange={(value) => updateKnowledge('content', value)} />
              <div className="announcement-actions">
                <button className="secondary" type="button" onClick={reloadKnowledge} disabled={busy}>Reload</button>
                <button className="secondary" type="button" onClick={resetKnowledge} disabled={busy}>Reset to template</button>
              </div>
            </Card>
          )}

          {section === 'mediaActions' && (
            <MediaActionsPanel
              actions={mediaActions}
              selectedAction={selectedMediaAction}
              rewards={channelPointRewards}
              busy={busy}
              onAdd={addMediaAction}
              onSelect={setSelectedMediaActionId}
              onUpdate={updateMediaAction}
              onRemove={removeMediaAction}
              onLoadRewards={loadChannelPointRewards}
              onImportAssets={importMediaActionAssets}
              onUpdateAssets={updateMediaActionAssets}
              onPreview={previewMediaAction}
              overlayUrl={mediaOverlayUrl}
            />
          )}

          {section === 'features' && (
            <div className="feature-stack">
              <FeaturePanel title="Chat" summary="Mentions, public commands, and cooldowns." defaultOpen>
                <div className="toggle-grid">
                  <Toggle label="Respond to mentions" checked={settings.enableMentions} onChange={(value) => update('enableMentions', value)} />
                  <Toggle label="Enable !ask" checked={settings.enableAsk} onChange={(value) => update('enableAsk', value)} />
                  <Toggle label="Enable !lurk" checked={settings.enableLurk} onChange={(value) => update('enableLurk', value)} />
                  <Toggle label="Enable !commands" checked={settings.enableCommands} onChange={(value) => update('enableCommands', value)} />
                  <Toggle label="Enable !reset" checked={settings.enableReset} onChange={(value) => update('enableReset', value)} />
                </div>
                <div className="split">
                  <SelectField label="Mention permission" value={settings.mentionPermission} options={permissionOptions} onChange={(value) => update('mentionPermission', value)} />
                  <SelectField label="!ask permission" value={settings.askPermission} options={permissionOptions} onChange={(value) => update('askPermission', value)} />
                </div>
                <div className="split">
                  <SelectField label="!lurk permission" value={settings.lurkPermission} options={permissionOptions} onChange={(value) => update('lurkPermission', value)} />
                  <SelectField label="!game permission" value={settings.gamePermission} options={permissionOptions} onChange={(value) => update('gamePermission', value)} />
                </div>
                <div className="split">
                  <SelectField label="!commands permission" value={settings.commandsPermission} options={permissionOptions} onChange={(value) => update('commandsPermission', value)} />
                  <SelectField label="!reset permission" value={settings.resetPermission} options={permissionOptions} onChange={(value) => update('resetPermission', value)} />
                </div>
                <div className="split">
                  <NumberField label="Global cooldown seconds" value={settings.globalCooldownSeconds} onChange={(value) => update('globalCooldownSeconds', value)} />
                  <NumberField label="User cooldown seconds" value={settings.userCooldownSeconds} onChange={(value) => update('userCooldownSeconds', value)} />
                </div>
              </FeaturePanel>
              <FeaturePanel title="!game snapshots" summary="Crop the Twitch preview before visual analysis.">
                <div className="info-callout">
                  <strong>Crop keeps Lupus focused on the game.</strong>
                  <span>These ratios trim chat, avatar, overlays, and player controls from the stream thumbnail before `!game analyze` uses Gemini vision.</span>
                </div>
                <Toggle label="Crop game snapshots" checked={settings.gameSnapshotCropEnabled} onChange={(value) => update('gameSnapshotCropEnabled', value)} />
                <div className="split">
                  <NumberField label="Crop X ratio" value={settings.gameSnapshotCropX} step={0.001} min={0} max={1} onChange={(value) => update('gameSnapshotCropX', value)} />
                  <NumberField label="Crop Y ratio" value={settings.gameSnapshotCropY} step={0.001} min={0} max={1} onChange={(value) => update('gameSnapshotCropY', value)} />
                </div>
                <div className="split">
                  <NumberField label="Crop width ratio" value={settings.gameSnapshotCropWidth} step={0.001} min={0.001} max={1} onChange={(value) => update('gameSnapshotCropWidth', value)} />
                  <NumberField label="Crop height ratio" value={settings.gameSnapshotCropHeight} step={0.001} min={0.001} max={1} onChange={(value) => update('gameSnapshotCropHeight', value)} />
                </div>
              </FeaturePanel>
              <FeaturePanel title="Shoutouts" summary="AutoSO queue, SO roulette pool, and timing." defaultOpen>
                <Toggle label="Enable !autoso and !soroulette" checked={settings.autosoEnabled} onChange={(value) => update('autosoEnabled', value)} />
                <div className="split">
                  <SelectField label="!autoso permission" value={settings.autosoPermission} options={permissionOptions} onChange={(value) => update('autosoPermission', value)} />
                  <SelectField label="!soroulette permission" value={settings.soRoulettePermission} options={permissionOptions} onChange={(value) => update('soRoulettePermission', value)} />
                </div>
                <div className="split">
                  <NumberField label="Minimum watch minutes" value={settings.recentStreamerMinWatch} onChange={(value) => update('recentStreamerMinWatch', value)} />
                  <NumberField label="Recent stream days" value={settings.recentStreamerDays} onChange={(value) => update('recentStreamerDays', value)} />
                </div>
                <div className="split">
                  <NumberField label="Page size" value={settings.recentStreamerPageSize} onChange={(value) => update('recentStreamerPageSize', value)} />
                  <NumberField label="Shoutout delay seconds" value={settings.recentStreamerDelay} min={1} onChange={(value) => update('recentStreamerDelay', value)} />
                </div>
                <TextArea label="!soroulette streamer pool" value={settings.soRouletteStreamers} onChange={(value) => update('soRouletteStreamers', value)} />
              </FeaturePanel>
              <FeaturePanel title="Ad alerts" summary="Scheduled ad warnings and fallback messages.">
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
              </FeaturePanel>
              <FeaturePanel title="Announcements" summary={`${announcements.length} configured command or timer messages.`}>
                <div className="split">
                  <Toggle label="Enable announcements" checked={settings.announcementsEnabled} onChange={(value) => update('announcementsEnabled', value)} />
                  <NumberField label="Timer poll seconds" value={settings.announcementPollSeconds} onChange={(value) => update('announcementPollSeconds', value)} />
                </div>
                <div className="announcement-actions">
                  <button type="button" onClick={() => addAnnouncement('command')}>Add command</button>
                  <button className="secondary" type="button" onClick={() => addAnnouncement('timer')}>Add timer</button>
                </div>
                {announcements.length === 0 ? (
                  <p className="muted">No announcements configured.</p>
                ) : (
                  <div className="announcement-sections">
                    <AnnouncementSummarySection
                      title="Timer Announcements"
                      kind="timer"
                      announcements={timerAnnouncements}
                      updateAnnouncement={updateAnnouncement}
                      removeAnnouncement={removeAnnouncement}
                    />
                    <AnnouncementSummarySection
                      title="Command Announcements"
                      kind="command"
                      announcements={commandAnnouncements}
                      updateAnnouncement={updateAnnouncement}
                      removeAnnouncement={removeAnnouncement}
                    />
                  </div>
                )}
              </FeaturePanel>
            </div>
          )}
        </section>

        {canSaveSection && (
          <footer className="actions">
            <button onClick={save} disabled={busy}>Save changes</button>
            <button className="secondary" onClick={() => refresh(!dirty)} disabled={busy}>Refresh</button>
          </footer>
        )}
      </div>
    </main>
  )
}

function MediaActionsPanel({
  actions,
  selectedAction,
  rewards,
  busy,
  onAdd,
  onSelect,
  onUpdate,
  onRemove,
  onLoadRewards,
  onImportAssets,
  onUpdateAssets,
  onPreview,
  overlayUrl
}: {
  actions: MediaAction[]
  selectedAction: MediaAction | null
  rewards: ChannelPointReward[]
  busy: boolean
  onAdd: () => void
  onSelect: (id: string) => void
  onUpdate: <K extends keyof MediaAction>(id: string, key: K, value: MediaAction[K]) => void
  onRemove: (id: string) => void
  onLoadRewards: () => void
  onImportAssets: (action: MediaAction, kind: 'media' | 'sound') => void
  onUpdateAssets: (actionId: string, kind: 'media' | 'sound', assets: MediaAsset[]) => void
  onPreview: (action: MediaAction) => void
  overlayUrl: string
}) {
  const rewardOptions = [
    { value: '', label: rewards.length === 0 ? 'Load rewards' : 'Choose redeem' },
    ...rewards.map((reward) => ({ value: reward.id, label: reward.enabled ? reward.title : `${reward.title} disabled` }))
  ]

  return (
    <div className="media-actions-layout">
      <section className="media-action-list">
        <div className="media-action-toolbar">
          <h3>Actions</h3>
          <button type="button" onClick={onAdd}>Add</button>
        </div>
        {actions.length === 0 ? (
          <p className="muted">No media actions configured.</p>
        ) : (
          <div className="media-action-cards">
            {actions.map((action) => (
              <button
                className={`media-action-card ${selectedAction?.id === action.id ? 'active' : ''}`}
                key={action.id}
                type="button"
                onClick={() => onSelect(action.id)}
              >
                <strong>{action.name || 'Untitled'}</strong>
                <span>{action.rewardTitle || 'Channel Point Redeem'}</span>
                <small>{action.media?.length ?? 0} media · {action.sounds?.length ?? 0} sounds · {action.duration || 5}s</small>
                <em>{action.enabled ? 'Enabled' : 'Disabled'}</em>
              </button>
            ))}
          </div>
        )}
      </section>

      {selectedAction ? (
        <section className="media-action-editor">
          <div className="overlay-url-panel">
            <div>
              <strong>OBS Browser Source</strong>
              <span>{overlayUrl || 'Overlay starting...'}</span>
            </div>
            <button className="secondary" type="button" onClick={() => navigator.clipboard?.writeText(overlayUrl)} disabled={!overlayUrl}>Copy</button>
          </div>

          <div className="media-editor-header">
            <div>
              <h3>{selectedAction.name || 'Untitled'}</h3>
              <span>{selectedAction.enabled ? 'Enabled' : 'Disabled'}</span>
            </div>
            <div className="media-editor-actions">
              <button type="button" onClick={() => onPreview(selectedAction)} disabled={busy}>Preview</button>
              <button className="danger" type="button" onClick={() => onRemove(selectedAction.id)}>Delete</button>
            </div>
          </div>

          <div className="media-editor-grid">
            <Card title="Setup">
              <TextField label="Name" value={selectedAction.name} onChange={(value) => onUpdate(selectedAction.id, 'name', value)} />
              <Toggle label="Enabled" checked={selectedAction.enabled} onChange={(value) => onUpdate(selectedAction.id, 'enabled', value)} />
              <SelectField label="Trigger" value={selectedAction.trigger} options={triggerOptions} onChange={(value) => onUpdate(selectedAction.id, 'trigger', value)} />
              <div className="reward-row">
                <SelectField
                  label="Redeem"
                  value={selectedAction.rewardId}
                  options={rewardOptions}
                  onChange={(value) => {
                    const reward = rewards.find((item) => item.id === value)
                    onUpdate(selectedAction.id, 'rewardId', value)
                    onUpdate(selectedAction.id, 'rewardTitle', reward?.title ?? selectedAction.rewardTitle)
                  }}
                />
                <button className="secondary" type="button" onClick={onLoadRewards} disabled={busy}>Refresh</button>
              </div>
            </Card>

            <Card title="Display">
              <NumberField label="Duration seconds" value={selectedAction.duration} min={1} max={60} onChange={(value) => onUpdate(selectedAction.id, 'duration', value)} />
              <SelectField label="Position" value={selectedAction.position} options={positionOptions} onChange={(value) => onUpdate(selectedAction.id, 'position', value)} />
              <label className="field">
                <span>Scale {selectedAction.scale}%</span>
                <input type="range" min={25} max={200} value={selectedAction.scale} onChange={(event) => onUpdate(selectedAction.id, 'scale', Number(event.target.value))} />
              </label>
              <SelectField label="Animation" value={selectedAction.animation} options={animationOptions} onChange={(value) => onUpdate(selectedAction.id, 'animation', value)} />
            </Card>
          </div>

          <div className="media-asset-grid">
            <AssetSection
              title="Media"
              kind="media"
              action={selectedAction}
              assets={selectedAction.media ?? []}
              onImport={() => onImportAssets(selectedAction, 'media')}
              onChange={(assets) => onUpdateAssets(selectedAction.id, 'media', assets)}
            />
            <AssetSection
              title="Sounds"
              kind="sound"
              action={selectedAction}
              assets={selectedAction.sounds ?? []}
              onImport={() => onImportAssets(selectedAction, 'sound')}
              onChange={(assets) => onUpdateAssets(selectedAction.id, 'sound', assets)}
            />
          </div>
        </section>
      ) : (
        <Card title="Media Actions" wide>
          <p className="muted">Create an action to connect a redeem to random media or sound.</p>
        </Card>
      )}
    </div>
  )
}

function AssetSection({
  title,
  kind,
  assets,
  onImport,
  onChange
}: {
  title: string
  kind: 'media' | 'sound'
  action: MediaAction
  assets: MediaAsset[]
  onImport: () => void
  onChange: (assets: MediaAsset[]) => void
}) {
  const move = (index: number, offset: number) => {
    const nextIndex = index + offset
    if (nextIndex < 0 || nextIndex >= assets.length) {
      return
    }
    const next = [...assets]
    const [item] = next.splice(index, 1)
    next.splice(nextIndex, 0, item)
    onChange(next)
  }

  const updateAsset = (id: string, patch: Partial<MediaAsset>) => {
    onChange(assets.map((asset) => (asset.id === id ? { ...asset, ...patch } : asset)))
  }

  return (
    <section className="asset-section">
      <div className="asset-section-header">
        <h3>{title}</h3>
        <button type="button" onClick={onImport}>Add</button>
      </div>
      {assets.length === 0 ? (
        <p className="muted">No {kind === 'media' ? 'media' : 'sounds'} added.</p>
      ) : (
        <div className="asset-list">
          {assets.map((asset, index) => (
            <div className="asset-row" key={asset.id}>
              <span className="drag-handle" aria-hidden="true">::</span>
              {kind === 'media' ? (
                <AssetThumbnail asset={asset} />
              ) : (
                <button className="secondary compact-button" type="button" onClick={() => playAsset(asset)}>Play</button>
              )}
              <div className="asset-name">
                <strong>{asset.filename}</strong>
                {kind === 'media' && (asset.durationMs || 0) > 0 ? <small>{formatAssetDuration(asset.durationMs || 0)}</small> : null}
                {kind === 'media' && isGifAsset(asset) ? (
                  <div className="gif-options">
                    <SelectField
                      label="GIF Playback"
                      value={asset.mediaPlaybackMode || 'normal'}
                      options={mediaPlaybackModeOptions}
                      onChange={(value) => updateAsset(asset.id, { mediaPlaybackMode: value })}
                      compact
                    />
                    <Toggle
                      label="Loop rotation"
                      checked={asset.excludeFromGifRotation !== true}
                      onChange={(value) => updateAsset(asset.id, { excludeFromGifRotation: !value })}
                    />
                  </div>
                ) : null}
              </div>
              <div className="asset-row-actions">
                <button className="secondary compact-button" type="button" onClick={() => move(index, -1)} disabled={index === 0}>Up</button>
                <button className="secondary compact-button" type="button" onClick={() => move(index, 1)} disabled={index === assets.length - 1}>Down</button>
                <button className="danger compact-button" type="button" onClick={() => onChange(assets.filter((item) => item.id !== asset.id))}>Delete</button>
              </div>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}

function AssetThumbnail({ asset }: { asset: MediaAsset }) {
  const [src, setSrc] = useState('')

  useEffect(() => {
    let alive = true
    GetMediaAssetDataURL(asset.path).then((value) => {
      if (alive) {
        setSrc(value)
      }
    }).catch(() => undefined)
    return () => {
      alive = false
    }
  }, [asset.path])

  return <div className="asset-thumb">{src ? <img src={src} alt="" /> : <span />}</div>
}

function playAsset(asset: MediaAsset) {
  GetMediaAssetDataURL(asset.path).then((value) => {
    const audio = new Audio(value)
    audio.play().catch(() => undefined)
  }).catch(() => undefined)
}

function formatAssetDuration(durationMs: number) {
  if (!durationMs || durationMs <= 0) {
    return ''
  }
  return `${(durationMs / 1000).toFixed(durationMs >= 10000 ? 0 : 1)}s GIF`
}

function isGifAsset(asset: MediaAsset) {
  return asset.filename.toLowerCase().endsWith('.gif') || asset.path.toLowerCase().endsWith('.gif')
}

function MediaActionOverlay({ playback }: { playback: MediaActionPlayback | null }) {
  if (!playback || !playback.mediaDataUrl) {
    return null
  }
  return (
    <div className={`media-overlay ${playback.position} ${playback.animation}`}>
      <AnimatedMediaImage playback={playback} />
    </div>
  )
}

function AnimatedMediaImage({ playback }: { playback: MediaActionPlayback }) {
  const frameSrc = useAnimatedMediaFrame(playback)
  return <img src={frameSrc || playback.mediaDataUrl} alt="" style={{ transform: `scale(${(playback.scale || 100) / 100})` }} />
}

function useAnimatedMediaFrame(playback: MediaActionPlayback) {
  const [src, setSrc] = useState(playback.mediaDataUrl)

  useEffect(() => {
    const cacheBusted = (url: string, token: number) => url ? `${url}#clip-${token}-${Date.now()}` : ''
    if (playback.mediaPlaybackMode === 'loop_next' && playback.mediaClips?.length) {
      let disposed = false
      let timer = 0
      const startedAt = Date.now()
      const durationMs = Math.max(1000, (playback.duration || 5) * 1000)
      let clipIndex = 0
      const playClip = () => {
        if (disposed) {
          return
        }
        const clip = playback.mediaClips?.[clipIndex] || playback.mediaClips?.[0]
        setSrc(cacheBusted(clip?.mediaDataUrl || playback.mediaDataUrl, clipIndex))
        const delay = Math.max(100, clip?.mediaDurationMs || 1000)
        clipIndex = (clipIndex + 1) % (playback.mediaClips?.length || 1)
        if (Date.now() - startedAt + delay >= durationMs) {
          return
        }
        timer = window.setTimeout(playClip, delay)
      }
      playClip()
      return () => {
        disposed = true
        if (timer) {
          window.clearTimeout(timer)
        }
      }
    }
    if (playback.mediaPlaybackMode !== 'match_audio') {
      setSrc(cacheBusted(playback.mediaDataUrl, 0))
      return
    }
    const clips = [{
      media: playback.media || { id: '', filename: '', path: '' },
      mediaDataUrl: playback.mediaDataUrl,
      mediaDurationMs: playback.mediaDurationMs,
      mediaFrameDataUrls: playback.mediaFrameDataUrls,
      mediaFrameDelaysMs: playback.mediaFrameDelaysMs
    }]
    let disposed = false
    let timer = 0
    const startedAt = Date.now()
    const durationMs = Math.max(1000, (playback.duration || 5) * 1000)
    const firstClipDuration = clipDurationMs(clips[0])
    const audio = playback.soundDataUrl ? new Audio(playback.soundDataUrl) : null
    const cleanup = () => {
      disposed = true
      if (timer) {
        window.clearTimeout(timer)
      }
    }
    const start = (targetDuration: number) => {
      const scale = playback.mediaPlaybackMode === 'match_audio' ? Math.max(0.1, targetDuration / Math.max(1, firstClipDuration)) : 1
      let clipIndex = 0
      let frameIndex = 0
      const tick = () => {
        if (disposed) {
          return
        }
        const clip = clips[clipIndex] || clips[0]
        const frames = clip.mediaFrameDataUrls || []
        const delays = clip.mediaFrameDelaysMs || []
        setSrc(frames[frameIndex] || clip.mediaDataUrl || playback.mediaDataUrl)
        const nextDelay = Math.max(10, delays[frameIndex] || 100) * scale
        frameIndex += 1
        if (frameIndex >= Math.max(1, frames.length)) {
          if (playback.mediaPlaybackMode === 'loop_next') {
            clipIndex = (clipIndex + 1) % clips.length
            frameIndex = 0
            if (Date.now() - startedAt >= durationMs) {
              return
            }
          } else if (playback.mediaPlaybackMode === 'loop') {
            frameIndex = 0
            if (Date.now() - startedAt >= durationMs) {
              return
            }
          } else {
            return
          }
        }
        timer = window.setTimeout(tick, nextDelay)
      }
      tick()
    }
    if (playback.mediaPlaybackMode === 'match_audio' && audio) {
      audio.addEventListener('loadedmetadata', () => start(Math.max(100, audio.duration * 1000)), { once: true })
      audio.load()
    } else {
      start(firstClipDuration)
    }
    return cleanup
  }, [playback])

  return src
}

function clipDurationMs(clip?: MediaPlaybackClip) {
  if (!clip) {
    return 1
  }
  return Math.max(1, clip.mediaDurationMs || (clip.mediaFrameDelaysMs || []).reduce((total, delay) => total + Math.max(10, delay || 100), 0))
}

function AnnouncementSummarySection({
  title,
  kind,
  announcements,
  updateAnnouncement,
  removeAnnouncement
}: {
  title: string
  kind: AnnouncementKind
  announcements: IndexedAnnouncement[]
  updateAnnouncement: AnnouncementUpdate
  removeAnnouncement: (index: number) => void
}) {
  const columns = kind === 'timer'
    ? ['ID', 'Type', 'First Send Minute', 'Repeat Interval']
    : ['ID', 'Type', 'Command']

  return (
    <section className="announcement-section">
      <h3>{title}</h3>
      {announcements.length === 0 ? (
        <p className="muted">No {kind} announcements configured.</p>
      ) : (
        <div className="announcement-table" role="table" aria-label={title}>
          <div className={`announcement-table-row announcement-table-head ${kind}`} role="row">
            {columns.map((column) => (
              <span role="columnheader" key={column}>{column}</span>
            ))}
          </div>
          <div className="announcement-table-body">
            {announcements.map(({ item, index }) => (
              <details className="announcement-row" key={`${item.id}-${index}`}>
                <summary className={`announcement-summary ${kind}`}>
                  <span>{item.id || 'Untitled'}</span>
                  <span>{item.kind === 'timer' ? 'Timer' : 'Command'}</span>
                  {kind === 'timer' ? (
                    <>
                      <span>{item.afterMinutes}</span>
                      <span>{item.repeatMinutes}</span>
                    </>
                  ) : (
                    <span>{item.command || '-'}</span>
                  )}
                </summary>
                <div className="announcement-edit">
                  <div className="announcement-row-header">
                    <Toggle label="Enabled" checked={item.enabled} onChange={(value) => updateAnnouncement(index, 'enabled', value)} />
                    <SelectField
                      label="Type"
                      value={item.kind}
                      options={announcementKindOptions}
                      onChange={(value) => updateAnnouncement(index, 'kind', value)}
                      compact
                    />
                    <button className="danger" type="button" onClick={() => removeAnnouncement(index)}>Remove</button>
                  </div>
                  <div className="split">
                    <TextField label="ID" value={item.id} onChange={(value) => updateAnnouncement(index, 'id', value)} />
                    {kind === 'timer' ? (
                      <>
                        <NumberField label="First send minute" value={item.afterMinutes} onChange={(value) => updateAnnouncement(index, 'afterMinutes', value)} />
                        <NumberField label="Repeat interval minutes" value={item.repeatMinutes} onChange={(value) => updateAnnouncement(index, 'repeatMinutes', value)} />
                      </>
                    ) : (
                      <>
                        <TextField label="Command" value={item.command} onChange={(value) => updateAnnouncement(index, 'command', value)} />
                        <SelectField label="Permission" value={item.permission || 'mods'} options={permissionOptions} onChange={(value) => updateAnnouncement(index, 'permission', value)} />
                      </>
                    )}
                  </div>
                  <TextArea label="Message" value={item.message} onChange={(value) => updateAnnouncement(index, 'message', value)} />
                </div>
              </details>
            ))}
          </div>
        </div>
      )}
    </section>
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

function FeaturePanel({
  title,
  summary,
  children,
  defaultOpen = false
}: {
  title: string
  summary: string
  children: React.ReactNode
  defaultOpen?: boolean
}) {
  return (
    <details className="feature-panel" open={defaultOpen}>
      <summary className="feature-panel-summary">
        <span>{title}</span>
        <small>{summary}</small>
      </summary>
      <div className="feature-panel-body">{children}</div>
    </details>
  )
}

function PermissionResult({ item }: { item: main.TwitchPermissionItem }) {
  const missingScopes = item.missingScopes ?? []
  return (
    <div className={`permission-result ${item.status}`}>
      <span className="permission-dot" aria-hidden="true" />
      <div>
        <strong>{item.name}</strong>
        <span>{item.detail}</span>
        {missingScopes.length > 0 && <code>{missingScopes.join(', ')}</code>}
      </div>
    </div>
  )
}

function formatCheckedAt(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString()
}

function TextField({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <label className="field">
      <span>{label}</span>
      <input value={value} onChange={(event) => onChange(event.target.value)} />
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

function NumberField({
  label,
  value,
  onChange,
  step,
  min,
  max
}: {
  label: string
  value: number
  onChange: (value: number) => void
  step?: number
  min?: number
  max?: number
}) {
  return (
    <label className="field">
      <span>{label}</span>
      <input type="number" value={value} step={step} min={min} max={max} onChange={(event) => onChange(Number(event.target.value))} />
    </label>
  )
}

function SelectField({
  label,
  value,
  options,
  onChange,
  compact = false
}: {
  label: string
  value: string
  options: Array<{ value: string; label: string }>
  onChange: (value: string) => void
  compact?: boolean
}) {
  const selected = options.find((option) => option.value === value) ?? options[0]

  return (
    <div className={`field select-field ${compact ? 'compact-field' : ''}`}>
      <span>{label}</span>
      <details className="select-menu">
        <summary className="select-trigger">
          <span>{selected?.label ?? value}</span>
        </summary>
        <div className="select-options">
          {options.map((option) => (
            <button
              className={option.value === value ? 'active' : ''}
              key={option.value}
              onClick={(event) => {
                onChange(option.value)
                const menu = event.currentTarget.closest('details')
                if (menu) {
                  menu.open = false
                }
              }}
              type="button"
            >
              {option.label}
            </button>
          ))}
        </div>
      </details>
    </div>
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

function StatusChip({
  label,
  value,
  tone = 'normal'
}: {
  label: string
  value: string
  tone?: 'normal' | 'good' | 'warning' | 'muted'
}) {
  return (
    <div className={`status-chip ${tone}`}>
      <span className="status-dot" aria-hidden="true" />
      <span className="status-chip-label">{label}</span>
      <strong>{value}</strong>
    </div>
  )
}
