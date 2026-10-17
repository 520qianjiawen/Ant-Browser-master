import { normalizeBackupPackageInfo, type BackupPackageInfo } from '../../packageInfo'

export interface GDriveConnection {
  configured: boolean
  clientID: string
  clientSecretConfigured: boolean
  accountEmail: string
  accountName: string
  accountAvatar: string
  folderName: string
  folderID: string
  proxyURL: string
  tokenExpiry: string
  autoSync: boolean
  lastSyncTime: string
}

export interface GDriveDraft {
  clientID: string
  clientSecret: string
  folderName: string
  proxyURL: string
  autoSync?: boolean
}

export interface GDriveBackupFile extends BackupPackageInfo {
  id: string
  name: string
  size: number
  modifiedAt: string
}

export interface GDriveDownloadResult {
  cancelled?: boolean
  localPath: string
  zipPath: string
  localDirectory?: string
  fileName: string
  size: number
  message?: string
}

export const defaultGDriveSettings: GDriveConnection = {
  configured: false,
  clientID: '',
  clientSecretConfigured: false,
  accountEmail: '',
  accountName: '',
  accountAvatar: '',
  folderName: 'AntBrowserBackup',
  folderID: '',
  proxyURL: '',
  tokenExpiry: '',
  autoSync: false,
  lastSyncTime: '',
}

const getBindings = async () => {
  const win = typeof window !== 'undefined' ? (window as any)?.go?.main?.App : null
  try {
    const mod = await import('../../../../wailsjs/go/main/App')
    return { ...win, ...mod }
  } catch {
    return win
  }
}

function normalizeGDriveSettings(raw: any): GDriveConnection {
  return {
    ...defaultGDriveSettings,
    configured: raw?.configured === true,
    clientID: typeof raw?.clientID === 'string' ? raw.clientID : '',
    clientSecretConfigured: raw?.clientSecretConfigured === true,
    accountEmail: typeof raw?.accountEmail === 'string' ? raw.accountEmail : '',
    accountName: typeof raw?.accountName === 'string' ? raw.accountName : '',
    accountAvatar: typeof raw?.accountAvatar === 'string' ? raw.accountAvatar : '',
    folderName: typeof raw?.folderName === 'string' && raw.folderName ? raw.folderName : defaultGDriveSettings.folderName,
    folderID: typeof raw?.folderID === 'string' ? raw.folderID : '',
    proxyURL: typeof raw?.proxyURL === 'string' ? raw.proxyURL : '',
    tokenExpiry: typeof raw?.tokenExpiry === 'string' ? raw.tokenExpiry : '',
    autoSync: raw?.autoSync === true,
    lastSyncTime: typeof raw?.lastSyncTime === 'string' ? raw.lastSyncTime : '',
  }
}

export async function fetchGDriveSettings(): Promise<GDriveConnection> {
  const bindings: any = await getBindings()
  if (!bindings?.BackupGDriveGetSettings) {
    return defaultGDriveSettings
  }
  return normalizeGDriveSettings(await bindings.BackupGDriveGetSettings())
}

export async function saveGDriveSettings(draft: GDriveDraft): Promise<GDriveConnection> {
  const bindings: any = await getBindings()
  if (!bindings?.BackupGDriveSaveSettings) {
    return defaultGDriveSettings
  }
  return normalizeGDriveSettings(await bindings.BackupGDriveSaveSettings({
    clientID: draft.clientID.trim(),
    clientSecret: draft.clientSecret.trim(),
    folderName: draft.folderName.trim(),
    proxyURL: draft.proxyURL.trim(),
  }))
}

export async function startGDriveOAuth(draft?: Partial<GDriveDraft>): Promise<{ authURL: string; port: number }> {
  const bindings: any = await getBindings()
  if (!bindings?.BackupGDriveStartOAuth) {
    throw new Error('Google Drive 授权接口未就绪')
  }
  return await bindings.BackupGDriveStartOAuth({
    clientID: draft?.clientID?.trim() || '',
    clientSecret: draft?.clientSecret?.trim() || '',
    proxyURL: draft?.proxyURL?.trim() || '',
  })
}

export async function revokeGDriveToken(): Promise<GDriveConnection> {
  const bindings: any = await getBindings()
  if (!bindings?.BackupGDriveRevokeToken) {
    return defaultGDriveSettings
  }
  return normalizeGDriveSettings(await bindings.BackupGDriveRevokeToken())
}

export async function testGDriveConnection(draft?: Partial<GDriveDraft>): Promise<{ ok: boolean; message: string; email?: string; displayName?: string }> {
  const bindings: any = await getBindings()
  if (!bindings?.BackupGDriveTest) {
    throw new Error('Google Drive 测试接口未就绪')
  }
  return await bindings.BackupGDriveTest(draft ? {
    clientID: draft.clientID?.trim() || '',
    clientSecret: draft.clientSecret?.trim() || '',
    proxyURL: draft.proxyURL?.trim() || '',
    folderName: draft.folderName?.trim() || '',
  } : {})
}

export async function listGDriveBackups(): Promise<GDriveBackupFile[]> {
  const bindings: any = await getBindings()
  if (!bindings?.BackupGDriveList) {
    return []
  }
  const raw = await bindings.BackupGDriveList({})
  if (!Array.isArray(raw)) return []

  return raw.map((item: any) => ({
    id: typeof item?.id === 'string' ? item.id : '',
    name: typeof item?.name === 'string' ? item.name : '',
    size: Number(item?.size) || 0,
    modifiedAt: typeof item?.modifiedAt === 'string' ? item.modifiedAt : '',
    ...normalizeBackupPackageInfo(typeof item?.name === 'string' ? item.name : ''),
  }))
}

export async function downloadGDriveBackup(fileNameOrID: string): Promise<GDriveDownloadResult> {
  const bindings: any = await getBindings()
  if (!bindings?.BackupGDriveDownload) {
    throw new Error('Google Drive 下载接口未就绪')
  }
  return await bindings.BackupGDriveDownload(fileNameOrID)
}

export async function deleteGDriveBackup(fileNameOrID: string): Promise<{ ok: boolean; message: string }> {
  const bindings: any = await getBindings()
  if (!bindings?.BackupGDriveDelete) {
    throw new Error('Google Drive 删除接口未就绪')
  }
  return await bindings.BackupGDriveDelete(fileNameOrID)
}

export async function syncGDriveNow(): Promise<{ ok: boolean; lastSyncTime: string; result?: any }> {
  const bindings: any = await getBindings()
  if (!bindings?.BackupGDriveSyncNow) {
    throw new Error('Google Drive 同步接口未就绪')
  }
  return await bindings.BackupGDriveSyncNow()
}

export async function restoreLatestGDriveBackup(): Promise<{ ok: boolean; fileName: string; modifiedAt: string; result?: any }> {
  const bindings: any = await getBindings()
  if (!bindings?.BackupGDriveRestoreLatest) {
    throw new Error('Google Drive 恢复接口未就绪')
  }
  return await bindings.BackupGDriveRestoreLatest()
}

export async function toggleGDriveAutoSync(enabled: boolean): Promise<GDriveConnection> {
  const bindings: any = await getBindings()
  if (!bindings?.BackupGDriveSaveSettings) {
    return defaultGDriveSettings
  }
  return normalizeGDriveSettings(await bindings.BackupGDriveSaveSettings({
    autoSync: enabled ? 'true' : 'false',
  }))
}

