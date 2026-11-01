import { useEffect, useState, useCallback } from 'react'
import { Check, Cpu, FolderOpen, Pencil, RotateCcw, Trash2 } from 'lucide-react'
import { Badge, Button, Card, ConfirmModal, Table, toast } from '../../../shared/components'
import type { TableColumn } from '../../../shared/components/Table'
import type { BrowserCore, BrowserCoreInput, BrowserCoreValidateResult, BrowserSettings, BrowserCoreExtended, BrowserProxy } from '../types'
import { fetchBrowserCores, saveBrowserCore, deleteBrowserCore, setDefaultBrowserCore, validateBrowserCorePath, openCorePath, fetchBrowserSettings, saveBrowserSettings, fetchCoreExtendedInfo, scanBrowserCores, importLocalBrowserCore, BrowserCoreDownload, fetchBrowserProxies, redownloadBrowserCore, fetchSunBrowserKernelInfo, downloadSunBrowserKernel } from '../api'
import { Environment, EventsOn, EventsOff } from '../../../wailsjs/runtime/runtime'
import { CoreDownloadModal } from './coreManagement/CoreDownloadModal'
import { CoreEditModal } from './coreManagement/CoreEditModal'
import { CoreSettingsCard } from './coreManagement/CoreSettingsCard'
import { CoreSettingsModal } from './coreManagement/CoreSettingsModal'
import { fetchCoreDownloadRecommendation } from './coreManagement/coreDownloadRecommendation'
import type { CoreDownloadRecommendation } from './coreManagement/coreDownloadRecommendation'
import type { CoreDisplayInfo, CoreDownloadForm, CoreDownloadProgress, CoreEditForm, CoreSettingsForm } from './coreManagement.types'

export function CoreManagementPage() {
  const [cores, setCores] = useState<BrowserCore[]>([])
  const [displayList, setDisplayList] = useState<CoreDisplayInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [scanning, setScanning] = useState(false)
  const [importing, setImporting] = useState(false)

  // 全局设置状态
  const [settings, setSettings] = useState<BrowserSettings>({
    userDataRoot: '',
    defaultFingerprintArgs: [],
    defaultLaunchArgs: [],
    defaultStartUrls: [],
    lightStartEnabled: true,
    restoreLastSession: false,
    clearCacheOnClose: false,
    startReadyTimeoutMs: 3000,
    startStableWindowMs: 1200,
    defaultConnectorType: 'xray',
  })
  const [settingsModalOpen, setSettingsModalOpen] = useState(false)
  const [settingsForm, setSettingsForm] = useState<CoreSettingsForm>({
    userDataRoot: '',
    defaultFingerprintArgs: '',
    defaultLaunchArgs: '',
    defaultStartUrls: '',
    lightStartEnabled: true,
    restoreLastSession: false,
    clearCacheOnClose: false,
    startReadyTimeoutMs: 3000,
    startStableWindowMs: 1200,
  })
  const [savingSettings, setSavingSettings] = useState(false)

  // 编辑弹窗状态
  const [editModalOpen, setEditModalOpen] = useState(false)
  const [editingCore, setEditingCore] = useState<BrowserCore | null>(null)
  const [editForm, setEditForm] = useState<CoreEditForm>({ coreName: '', corePath: '' })
  const [saving, setSaving] = useState(false)
  const [pathValidating, setPathValidating] = useState(false)
  const [pathValidResult, setPathValidResult] = useState<BrowserCoreValidateResult | null>(null)

  // 删除确认状态
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false)
  const [deletingCore, setDeletingCore] = useState<CoreDisplayInfo | null>(null)

  // 内核下载
  const [downloadModalOpen, setDownloadModalOpen] = useState(false)
  const [downloadForm, setDownloadForm] = useState<CoreDownloadForm>({ name: '', url: '', proxyMode: 'system', proxyId: '', mode: 'download' })
  const [downloadProgress, setDownloadProgress] = useState<CoreDownloadProgress | null>(null)
  const [downloadRecommendation, setDownloadRecommendation] = useState<CoreDownloadRecommendation | null>(null)
  const [downloadRecommendationLoading, setDownloadRecommendationLoading] = useState(false)
  const [downloadRecommendationError, setDownloadRecommendationError] = useState('')
  const [importProgress, setImportProgress] = useState<CoreDownloadProgress | null>(null)
  const [proxies, setProxies] = useState<BrowserProxy[]>([])
  const [sunKernelStarting, setSunKernelStarting] = useState(false)

  useEffect(() => {
    loadData()

    // 监听下载进度
    const onDownloadProgress = (data: { phase: string; progress: number; message: string }) => {
      setDownloadProgress(data)
      if (data.phase === 'done') {
        toast.success(data.message)
        setTimeout(() => {
          setDownloadModalOpen(false)
          setDownloadProgress(null)
          loadData() // 更新内核列表
        }, 1500)
      } else if (data.phase === 'error') {
        toast.error(data.message)
        setDownloadProgress(null) // 清理进度使其可以重新开始
      }
    }
    EventsOn('download:progress', onDownloadProgress)

    const onImportProgress = (data: { phase: string; progress: number; message: string }) => {
      setImportProgress(data)
      if (data.phase === 'done' || data.phase === 'error') {
        setTimeout(() => setImportProgress(null), 1200)
      }
    }
    EventsOn('core-import:progress', onImportProgress)

    return () => {
      EventsOff('download:progress')
      EventsOff('core-import:progress')
    }
  }, [])

  const loadData = async () => {
    setLoading(true)
    try {
      // 并行加载设置、内核列表和扩展信息
      const [settingsData, coreList, extendedInfo] = await Promise.all([
        fetchBrowserSettings(),
        fetchBrowserCores(),
        fetchCoreExtendedInfo(),
      ])

      setSettings(settingsData)
      setCores(coreList)

      // 创建扩展信息映射
      const extendedMap = new Map<string, BrowserCoreExtended>()
      extendedInfo.forEach(info => extendedMap.set(info.coreId, info))

      // 验证所有路径并合并扩展信息
      const displayInfoList: CoreDisplayInfo[] = await Promise.all(
        coreList.map(async (core) => {
          const result = await validateBrowserCorePath(core.corePath)
          const extended = extendedMap.get(core.coreId)
          return {
            coreId: core.coreId,
            coreName: core.coreName,
            corePath: core.corePath,
            isDefault: core.isDefault,
            pathValid: result.valid,
            pathMessage: result.message,
            chromeVersion: extended?.chromeVersion || '',
            instanceCount: extended?.instanceCount || 0,
          }
        })
      )
      setDisplayList(displayInfoList)
    } finally {
      setLoading(false)
    }
  }

  // 防抖验证路径
  const validatePath = useCallback(async (path: string) => {
    if (!path.trim()) {
      setPathValidResult(null)
      return
    }
    setPathValidating(true)
    try {
      const result = await validateBrowserCorePath(path)
      setPathValidResult(result)
    } finally {
      setPathValidating(false)
    }
  }, [])

  // 路径输入变化时触发验证（防抖）
  useEffect(() => {
    fetchBrowserProxies().then(setProxies)
    const timer = setTimeout(() => {
      if (editModalOpen && editForm.corePath) {
        validatePath(editForm.corePath)
      }
    }, 500)
    return () => clearTimeout(timer)
  }, [editForm.corePath, editModalOpen, validatePath])

  // 表格列定义
  const columns: TableColumn<CoreDisplayInfo>[] = [
    {
      key: 'coreName',
      title: '内核名称',
      width: '180px',
      render: (val) => (
        <div className="flex items-center gap-2 whitespace-nowrap">
          <div className="w-7 h-7 rounded-lg bg-[var(--color-bg-muted)] flex items-center justify-center text-[var(--color-text-muted)] shrink-0">
            <Cpu className="w-3.5 h-3.5" />
          </div>
          <span className="font-semibold text-sm text-[var(--color-text-primary)] truncate" title={val}>
            {val}
          </span>
        </div>
      ),
    },
    {
      key: 'corePath',
      title: '内核执行路径',
      render: (val) => (
        <span className="font-mono text-xs text-[var(--color-text-muted)] truncate block max-w-sm" title={val}>
          {val}
        </span>
      ),
    },
    {
      key: 'chromeVersion',
      title: 'Chrome 版本',
      width: '130px',
      render: (val) => (
        <span className="whitespace-nowrap font-mono text-xs text-[var(--color-text-secondary)]">
          {val || '-'}
        </span>
      ),
    },
    {
      key: 'instanceCount',
      title: '使用实例',
      width: '100px',
      align: 'center',
      render: (val) => (
        <Badge variant="default" size="sm" className="whitespace-nowrap font-mono">
          {val} 个
        </Badge>
      ),
    },
    {
      key: 'isDefault',
      title: '默认内核',
      width: '100px',
      align: 'center',
      render: (val) => (
        <span className="whitespace-nowrap inline-flex justify-center">
          {val ? (
            <Badge variant="info" size="sm" dot>默认</Badge>
          ) : (
            <span className="text-xs text-[var(--color-text-muted)]">-</span>
          )}
        </span>
      ),
    },
    {
      key: 'pathValid',
      title: '状态',
      width: '90px',
      align: 'center',
      render: (val) => (
        <Badge variant={val ? 'success' : 'error'} size="sm" dot className="whitespace-nowrap">
          {val ? '可用' : '失效'}
        </Badge>
      ),
    },
    {
      key: 'actions',
      title: '操作',
      width: '270px',
      align: 'right',
      render: (_, record) => (
        <div className="flex items-center justify-end gap-1.5 whitespace-nowrap">
          <Button
            size="sm"
            variant="ghost"
            onClick={(e) => { e.stopPropagation(); handleOpenPath(record.corePath) }}
            title="打开所在目录"
            className="px-2"
          >
            <FolderOpen className="w-3.5 h-3.5" />
          </Button>

          <Button
            size="sm"
            variant="ghost"
            onClick={(e) => { e.stopPropagation(); handleEdit(record) }}
            title="编辑内核"
            className="px-2 text-xs"
          >
            <Pencil className="w-3.5 h-3.5 mr-1" />
            <span>编辑</span>
          </Button>

          <Button
            size="sm"
            variant="ghost"
            onClick={(e) => { e.stopPropagation(); handleRedownload(record) }}
            title="重新下载并覆盖当前内核"
            className="px-2 text-xs"
          >
            <RotateCcw className="w-3.5 h-3.5 mr-1" />
            <span>重下</span>
          </Button>

          {!record.isDefault ? (
            <Button
              size="sm"
              variant="ghost"
              onClick={(e) => { e.stopPropagation(); handleSetDefault(record.coreId) }}
              title="设为默认内核"
              className="px-2 text-xs text-[var(--color-accent)] hover:bg-[var(--color-accent-muted)]"
            >
              <Check className="w-3.5 h-3.5 mr-1" />
              <span>设为默认</span>
            </Button>
          ) : (
            <div className="w-[74px]" />
          )}

          <Button
            size="sm"
            variant="ghost"
            onClick={(e) => { e.stopPropagation(); handleDeleteClick(record) }}
            title={record.isDefault ? '默认内核不能删除' : '删除内核'}
            disabled={record.isDefault}
            className="px-2 text-xs text-red-500 hover:text-red-600 hover:bg-red-500/10 disabled:opacity-40"
          >
            <Trash2 className="w-3.5 h-3.5" />
          </Button>
        </div>
      ),
    },
  ]

  // 打开内核路径
  const handleOpenPath = async (corePath: string) => {
    try {
      await openCorePath(corePath)
    } catch (error: any) {
      toast.error(error?.message || '打开目录失败')
    }
  }

  // 扫描 chrome 目录，自动注册新内核
  const handleScan = async () => {
    setScanning(true)
    try {
      await scanBrowserCores()
      await loadData()
      toast.success('扫描完成')
    } catch (error: any) {
      toast.error(error?.message || '扫描失败')
    } finally {
      setScanning(false)
    }
  }

  const handleImportLocal = async () => {
    setImporting(true)
    setImportProgress({ phase: 'selecting', progress: 0, message: '请选择本地内核包...' })
    try {
      const imported = await importLocalBrowserCore()
      if (!imported) {
        setImportProgress(null)
        return
      }
      await loadData()
      toast.success(`已导入：${imported.coreName}`)
    } catch (error: any) {
      toast.error(error?.message || '导入失败')
    } finally {
      setImporting(false)
    }
  }

  // 新增内核
  const handleAdd = () => {
    setEditingCore(null)
    setEditForm({ coreName: '', corePath: '' })
    setPathValidResult(null)
    setEditModalOpen(true)
  }

  // 编辑内核
  const handleEdit = (record: CoreDisplayInfo) => {
    const core = cores.find(c => c.coreId === record.coreId)
    if (core) {
      setEditingCore(core)
      setEditForm({ coreName: core.coreName, corePath: core.corePath })
      setPathValidResult({ valid: record.pathValid, message: record.pathMessage })
      setEditModalOpen(true)
    }
  }

  const loadCoreDownloadRecommendation = async () => {
    setDownloadRecommendationLoading(true)
    setDownloadRecommendationError('')
    try {
      let runtimeEnv: { platform?: string; arch?: string } | null = null
      try {
        runtimeEnv = await Environment()
      } catch {
        runtimeEnv = null
      }
      const recommendation = await fetchCoreDownloadRecommendation(runtimeEnv)
      setDownloadRecommendation(recommendation)
      if (!recommendation) {
        setDownloadRecommendationError('当前环境没有可自动解压的 Release 压缩包')
        return
      }
      setDownloadForm(prev => ({
        ...prev,
        name: prev.name.trim() ? prev.name : (recommendation.namePlaceholder || 'chrome-152'),
        url: prev.url.trim() ? prev.url : recommendation.downloadUrl,
      }))
    } catch (error: any) {
      setDownloadRecommendation(null)
      setDownloadRecommendationError(error?.message || '获取推荐地址失败')
    } finally {
      setDownloadRecommendationLoading(false)
    }
  }

  const handleOpenDownload = () => {
    setDownloadForm({ name: '', url: '', proxyMode: 'system', proxyId: '', mode: 'download' })
    setDownloadProgress(null)
    setDownloadRecommendation(null)
    setDownloadRecommendationError('')
    setDownloadModalOpen(true)
    void loadCoreDownloadRecommendation()
  }

  const handleRedownload = (record: CoreDisplayInfo) => {
    setDownloadForm({ coreId: record.coreId, name: record.coreName, url: '', proxyMode: 'system', proxyId: '', mode: 'redownload' })
    setDownloadProgress(null)
    setDownloadRecommendation(null)
    setDownloadRecommendationError('')
    setDownloadModalOpen(true)
    void loadCoreDownloadRecommendation()
  }

  // SunBrowser 一键下载：先查最新版本信息，确认后走标准下载管线
  const handleSunBrowserOneClick = async () => {
    if (sunKernelStarting) return
    setSunKernelStarting(true)
    setDownloadProgress({ phase: 'starting', progress: 0, message: '正在查询 SunBrowser 最新内核...' })
    try {
      const info = await fetchSunBrowserKernelInfo('__system__')
      if (!info.available) {
        setDownloadProgress(null)
        toast.error(info.message || '未获取到可用内核')
        return
      }
      // 检查是否已安装同名内核
      const installed = displayList.some(c => c.coreName.toLowerCase() === info.coreName.toLowerCase())
      const confirmText = installed
        ? `检测到已存在内核「${info.coreName}」\n\n最新版本: ${info.version}\n大小: ${info.sizeMb || '未知'}\n校验: MD5 ${info.fileMd5 || '官方未提供'}\n\n是否重新下载并覆盖更新内核？`
        : `发现 SunBrowser 内核最新版\n\n版本: ${info.version}\n大小: ${info.sizeMb || '未知'}\n校验: MD5 ${info.fileMd5 || '官方未提供'}\n\n将自动下载、校验 MD5 并注册为「${info.coreName}」，继续吗？`

      const ok = window.confirm(confirmText)
      if (!ok) {
        setDownloadProgress(null)
        setSunKernelStarting(false)
        return
      }
      setDownloadProgress({ phase: 'downloading', progress: 0, message: `开始下载 SunBrowser ${info.version}...` })
      await downloadSunBrowserKernel('__system__')
      // 进度由 download:progress 事件驱动；done 分支统一刷新
    } catch (err: any) {
      setDownloadProgress(null)
      toast.error(err?.message || 'SunBrowser 一键下载失败')
    } finally {
      setSunKernelStarting(false)
    }
  }

  // 保存内核
  const handleSaveCore = async () => {
    if (!editForm.coreName.trim()) {
      toast.error('请输入内核名称')
      return
    }
    if (!editForm.corePath.trim()) {
      toast.error('请输入内核路径')
      return
    }
    setSaving(true)
    try {
      const input: BrowserCoreInput = {
        coreId: editingCore?.coreId || `core-${Date.now()}`,
        coreName: editForm.coreName.trim(),
        corePath: editForm.corePath.trim(),
        isDefault: editingCore?.isDefault || false,
      }
      await saveBrowserCore(input)
      await loadData()
      setEditModalOpen(false)
      toast.success(editingCore ? '内核已更新' : '内核已添加')
    } catch (error: any) {
      toast.error(error?.message || '保存失败')
    } finally {
      setSaving(false)
    }
  }

  // 删除点击
  const handleDeleteClick = (record: CoreDisplayInfo) => {
    if (record.isDefault) {
      toast.warning('默认内核不能删除')
      return
    }
    setDeletingCore(record)
    setDeleteConfirmOpen(true)
  }

  // 确认删除
  const handleDeleteConfirm = async () => {
    if (!deletingCore) return
    try {
      await deleteBrowserCore(deletingCore.coreId)
      await loadData()
      toast.success('内核已删除')
    } catch (error: any) {
      toast.error(error?.message || '删除失败')
    }
    setDeletingCore(null)
  }

  // 设为默认
  const handleSetDefault = async (coreId: string) => {
    try {
      await setDefaultBrowserCore(coreId)
      await loadData()
      toast.success('已设为默认内核')
    } catch (error: any) {
      toast.error(error?.message || '设置失败')
    }
  }

  // 开始下载
  const handleStartDownloadCore = async () => {
    if (!downloadForm.name.trim() || !downloadForm.url.trim()) {
      toast.error('请输入名称和下载地址')
      return
    }
    if (downloadForm.mode === 'redownload' && !downloadForm.coreId) {
      toast.error('缺少内核ID')
      return
    }
    setDownloadProgress({ phase: 'starting', progress: 0, message: '准备下载...' })
    try {
      // 在这儿我们需要从 proxies 中寻找匹配到的代理设定，如果有则传过去的 url
      let targetProxy = ''
      if (downloadForm.proxyMode === 'system') {
        targetProxy = '__system__'
      } else if (downloadForm.proxyMode === 'direct') {
        targetProxy = '__direct__'
      } else {
        const proxyProfile = proxies.find(p => p.proxyId === downloadForm.proxyId)
        targetProxy = downloadForm.proxyId
        if (proxyProfile && proxyProfile.proxyConfig) {
          targetProxy = proxyProfile.proxyConfig
        }
      }

      if (downloadForm.mode === 'redownload') {
        await redownloadBrowserCore(downloadForm.coreId || '', downloadForm.url.trim(), targetProxy)
      } else {
        await BrowserCoreDownload(downloadForm.name.trim(), downloadForm.url.trim(), targetProxy)
      }
    } catch (err: any) {
      toast.error(err.message || '内部启动下载失败')
      setDownloadProgress(null)
    }
  }

  // 打开设置编辑弹窗
  const handleEditSettings = () => {
    setSettingsForm({
      userDataRoot: settings.userDataRoot,
      defaultFingerprintArgs: settings.defaultFingerprintArgs.join('\n'),
      defaultLaunchArgs: settings.defaultLaunchArgs.join('\n'),
      defaultStartUrls: settings.defaultStartUrls.join('\n'),
      lightStartEnabled: settings.lightStartEnabled,
      restoreLastSession: settings.restoreLastSession,
      clearCacheOnClose: settings.clearCacheOnClose,
      startReadyTimeoutMs: settings.startReadyTimeoutMs,
      startStableWindowMs: settings.startStableWindowMs,
    })
    setSettingsModalOpen(true)
  }

  // 保存设置
  const handleSaveSettings = async () => {
    setSavingSettings(true)
    try {
      const newSettings: BrowserSettings = {
        userDataRoot: settingsForm.userDataRoot.trim(),
        defaultFingerprintArgs: settingsForm.defaultFingerprintArgs.split('\n').map(s => s.trim()).filter(Boolean),
        defaultLaunchArgs: settingsForm.defaultLaunchArgs.split('\n').map(s => s.trim()).filter(Boolean),
        defaultStartUrls: settingsForm.defaultStartUrls.split('\n').map(s => s.trim()).filter(Boolean),
        lightStartEnabled: settingsForm.lightStartEnabled,
        restoreLastSession: settingsForm.restoreLastSession,
        clearCacheOnClose: settingsForm.clearCacheOnClose,
        startReadyTimeoutMs: Math.max(1000, Number(settingsForm.startReadyTimeoutMs) || 3000),
        startStableWindowMs: Math.max(0, Number(settingsForm.startStableWindowMs) || 1200),
        defaultConnectorType: settings.defaultConnectorType || 'xray',
      }
      await saveBrowserSettings(newSettings)
      setSettings(newSettings)
      setSettingsModalOpen(false)
      toast.success('设置已保存')
    } catch (error: any) {
      toast.error(error?.message || '保存失败')
    } finally {
      setSavingSettings(false)
    }
  }


  return (
    <div className="space-y-5 animate-fade-in">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-[var(--color-text-primary)]">内核管理</h1>
          <p className="text-sm text-[var(--color-text-muted)] mt-1">管理 Chrome 内核版本和全局设置</p>
        </div>
        <div className="flex gap-2">
          <Button size="sm" variant="secondary" onClick={handleSunBrowserOneClick} loading={sunKernelStarting}>
            一键下载 SunBrowser
          </Button>
          <Button size="sm" variant="secondary" onClick={handleOpenDownload}>下载内核</Button>
          <Button size="sm" variant="secondary" onClick={handleImportLocal} loading={importing}>导入本地</Button>
          <Button size="sm" variant="secondary" onClick={handleScan} loading={scanning}>扫描内核</Button>
          <Button size="sm" onClick={handleAdd}>新增内核</Button>
        </div>
      </div>

      {importProgress && (
        <div className="flex items-center justify-between rounded-lg border border-[var(--color-border-default)] bg-[var(--color-bg-secondary)] px-3 py-2 text-sm">
          <span className="text-[var(--color-text-secondary)]">{importProgress.message}</span>
          <span className="text-[var(--color-text-muted)]">{Math.max(0, Math.min(100, importProgress.progress))}%</span>
        </div>
      )}

      {downloadProgress && !downloadModalOpen && (
        <div className="flex items-center justify-between rounded-lg border border-[var(--color-border-default)] bg-[var(--color-bg-secondary)] px-4 py-3 text-sm">
          <div className="flex items-center gap-3">
            <div className="w-4 h-4 border-2 border-[var(--color-border-default)] border-t-[var(--color-accent)] rounded-full animate-spin flex-shrink-0" />
            <span className="text-[var(--color-text-secondary)] font-medium">{downloadProgress.message}</span>
          </div>
          <span className="text-[var(--color-text-muted)] font-mono">{Math.max(0, Math.min(100, downloadProgress.progress))}%</span>
        </div>
      )}

      <CoreSettingsCard settings={settings} onEdit={handleEditSettings} />

      {/* 内核列表卡片 */}
      <Card title="内核列表" subtitle="已配置的 Chrome 内核">
        <Table
          columns={columns}
          data={displayList}
          rowKey="coreId"
          loading={loading}
          tableMinWidth="980px"
          emptyText="暂无内核，请添加内核"
        />
      </Card>

      <CoreSettingsModal
        open={settingsModalOpen}
        form={settingsForm}
        saving={savingSettings}
        setForm={setSettingsForm}
        onClose={() => setSettingsModalOpen(false)}
        onSave={handleSaveSettings}
      />

      <CoreEditModal
        open={editModalOpen}
        isEditing={Boolean(editingCore)}
        form={editForm}
        saving={saving}
        pathValidating={pathValidating}
        pathValidResult={pathValidResult}
        setForm={setEditForm}
        onClose={() => setEditModalOpen(false)}
        onSave={handleSaveCore}
      />

      {/* 删除确认弹窗 */}
      <ConfirmModal
        open={deleteConfirmOpen}
        onClose={() => setDeleteConfirmOpen(false)}
        onConfirm={handleDeleteConfirm}
        title="确认删除"
        content={`确定要删除内核"${deletingCore?.coreName}"吗？此操作不可恢复。`}
        confirmText="删除"
        danger
      />

      <CoreDownloadModal
        open={downloadModalOpen}
        form={downloadForm}
        progress={downloadProgress}
        recommendation={downloadRecommendation}
        recommendationLoading={downloadRecommendationLoading}
        recommendationError={downloadRecommendationError}
        proxies={proxies}
        setForm={setDownloadForm}
        setProgress={setDownloadProgress}
        onRefreshRecommendation={loadCoreDownloadRecommendation}
        onClose={() => setDownloadModalOpen(false)}
        onStart={handleStartDownloadCore}
      />
    </div>
  )
}
