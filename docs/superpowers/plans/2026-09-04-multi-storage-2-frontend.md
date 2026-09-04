# Multi Storage Destination — Chunk 2: Frontend (SPA)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** SPA memakai endpoint destination baru: Settings mengelola banyak destination S3, DumpModal memilih destination, Dumps menampilkan nama destination. RestoreModal tidak berubah.

**Architecture:** Pola mengikuti `DatabaseModal` (create/edit satu komponen, `secretKey` kosong = keep existing). Query key baru: `['storage-destinations']`.

**Tech Stack:** React 19, TanStack Query, Tailwind, lucide-react.

**Prerequisite:** Chunk 1 (backend) sudah merged — endpoint `/api/storage/destinations*` dan DTO `destId`/`destName` tersedia.

## Global Constraints

- Secret TIDAK pernah dirender; yang tampil hanya badge `secretSet`.
- `destId` string kosong (`''`) berarti Local.
- Verifikasi: `npm --prefix web run build` (tsc + vite) harus lulus di setiap task akhir.
- Tidak ada file backend yang disentuh.

---

### Task 1: types.ts

**Files:**
- Modify: `web/src/lib/types.ts`

- [ ] **Step 1: Edit DTO**

`DumpDTO`: ganti `storage: 'local' | 's3'` dengan:

```ts
  destId: string
  destName: string
```

`JobDTO`: hapus baris `storage: string | null`.

Hapus `StorageSettingsDTO`, tambah:

```ts
export interface StorageDestinationDTO {
  id: string
  name: string
  kind: 's3'
  endpoint: string
  region: string
  bucket: string
  prefix: string
  accessKey: string
  secretSet: boolean
  createdAt: string
}
```

- [ ] **Step 2: Commit** (build akan merah sampai Task 4 — commit terpisah tetap oke karena satu PR chunk)

```bash
git add web/src/lib/types.ts
git commit -m "feat(web): storage destination DTO types"
```

---

### Task 2: DestinationModal + halaman Settings

**Files:**
- Create: `web/src/modals/DestinationModal.tsx`
- Modify: `web/src/pages/Settings.tsx`

- [ ] **Step 1: Buat `DestinationModal.tsx`** (pola `DatabaseModal`):

```tsx
import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { api, ApiError } from '../lib/api'
import type { StorageDestinationDTO, TestResultDTO } from '../lib/types'
import { Button, Field, Input, Modal, Spinner } from '../ui'

interface Props {
  open: boolean
  onClose: () => void
  dest?: StorageDestinationDTO
  onSaved: () => void
}

interface FormState {
  name: string
  endpoint: string
  region: string
  bucket: string
  prefix: string
  accessKey: string
  secretKey: string
}

const emptyForm: FormState = {
  name: '', endpoint: '', region: '', bucket: '', prefix: '', accessKey: '', secretKey: '',
}

export default function DestinationModal({ open, onClose, dest, onSaved }: Props) {
  const [form, setForm] = useState<FormState>(emptyForm)
  const [error, setError] = useState('')
  const [testMsg, setTestMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const [testing, setTesting] = useState(false)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    setError('')
    setTestMsg(null)
    if (dest) {
      setForm({
        name: dest.name, endpoint: dest.endpoint, region: dest.region,
        bucket: dest.bucket, prefix: dest.prefix, accessKey: dest.accessKey, secretKey: '',
      })
    } else {
      setForm(emptyForm)
    }
  }, [open, dest])

  function set<K extends keyof FormState>(key: K, value: string) {
    setForm((f) => ({ ...f, [key]: value }))
  }

  async function onTest() {
    setTesting(true)
    setTestMsg(null)
    try {
      const res = await api.post<TestResultDTO>('/api/storage/destinations/test', form)
      setTestMsg(res.ok ? { ok: true, text: 'Connection OK' } : { ok: false, text: `Failed: ${res.error ?? 'unknown'}` })
    } catch (err) {
      setTestMsg({ ok: false, text: err instanceof ApiError ? err.message : 'test failed' })
    } finally {
      setTesting(false)
    }
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setSaving(true)
    try {
      if (dest) await api.put(`/api/storage/destinations/${dest.id}`, form)
      else await api.post('/api/storage/destinations', form)
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'save failed')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={dest ? `Edit ${dest.name}` : 'Add S3 Destination'}
      subtitle="Register an S3-compatible bucket (AWS S3, MinIO, R2, Spaces) as a dump destination."
    >
      <form onSubmit={onSubmit} className="space-y-4">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field label="Display Name">
            <Input value={form.name} onChange={(e) => set('name', e.target.value)} placeholder="MinIO Offsite" required />
          </Field>
          <Field label="Endpoint URL" hint="e.g. http://127.0.0.1:9000">
            <Input value={form.endpoint} onChange={(e) => set('endpoint', e.target.value)} placeholder="https://s3.us-east-1.amazonaws.com" required />
          </Field>
          <Field label="Region">
            <Input value={form.region} onChange={(e) => set('region', e.target.value)} placeholder="us-east-1" />
          </Field>
          <Field label="Bucket Name">
            <Input value={form.bucket} onChange={(e) => set('bucket', e.target.value)} placeholder="my-db-backups" required />
          </Field>
          <Field label="Key Prefix (Optional)">
            <Input value={form.prefix} onChange={(e) => set('prefix', e.target.value)} placeholder="ku-dump/production" />
          </Field>
          <Field label="Access Key ID">
            <Input value={form.accessKey} onChange={(e) => set('accessKey', e.target.value)} required />
          </Field>
        </div>
        <Field label={dest ? 'Secret Access Key (leave blank to keep existing)' : 'Secret Access Key'}>
          <Input type="password" value={form.secretKey} onChange={(e) => set('secretKey', e.target.value)} required={!dest} />
        </Field>

        {error && (
          <div className="rounded-lg border border-rose-800/60 bg-rose-950/40 p-2.5 text-xs text-rose-300">{error}</div>
        )}
        {testMsg && (
          <div
            className={`rounded-lg border p-2.5 text-xs ${
              testMsg.ok ? 'border-emerald-800/60 bg-emerald-950/40 text-emerald-300' : 'border-rose-800/60 bg-rose-950/40 text-rose-300'
            }`}
          >
            {testMsg.text}
          </div>
        )}

        <div className="flex items-center justify-between border-t border-slate-800 pt-4">
          {!dest && (
            <Button type="button" variant="outline" onClick={onTest} disabled={testing}>
              {testing ? <Spinner /> : 'Test Connection'}
            </Button>
          )}
          {dest && <span className="text-[11px] text-slate-500">Test saved config from the row button.</span>}
          <div className="flex items-center gap-2">
            <Button type="button" variant="ghost" onClick={onClose}>Cancel</Button>
            <Button type="submit" disabled={saving}>{saving ? <Spinner /> : 'Save Destination'}</Button>
          </div>
        </div>
      </form>
    </Modal>
  )
}
```

- [ ] **Step 2: Ganti section S3 di `Settings.tsx`**

Hapus seluruh state/form S3 lama (`endpoint`, `region`, dst. + `onSave`/`onTest`) dan section form; ganti dengan daftar destination:

```tsx
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, CheckCircle2, Cloud, KeyRound, Plus, Terminal, Trash2, Pencil } from 'lucide-react'
import { api, ApiError } from '../lib/api'
import type { StorageDestinationDTO, TestResultDTO, ToolsDTO } from '../lib/types'
import { Badge, Button, Spinner } from '../ui'
import DestinationModal from '../modals/DestinationModal'

export default function Settings() {
  const qc = useQueryClient()
  const [editing, setEditing] = useState<StorageDestinationDTO | undefined>(undefined)
  const [showAdd, setShowAdd] = useState(false)
  const [testMsg, setTestMsg] = useState<{ id: string; ok: boolean; text: string } | null>(null)
  const [deleteErr, setDeleteErr] = useState('')

  const dests = useQuery({
    queryKey: ['storage-destinations'],
    queryFn: () => api.get<StorageDestinationDTO[]>('/api/storage/destinations'),
  })
  const tools = useQuery({ queryKey: ['tools'], queryFn: () => api.get<ToolsDTO>('/api/tools') })

  const invalidate = () => void qc.invalidateQueries({ queryKey: ['storage-destinations'] })

  const remove = useMutation({
    mutationFn: (id: string) => api.del(`/api/storage/destinations/${id}`),
    onSuccess: () => { setDeleteErr(''); invalidate() },
    onError: (err) => setDeleteErr(err instanceof ApiError ? err.message : 'delete failed'),
  })

  async function testDest(d: StorageDestinationDTO) {
    setTestMsg({ id: d.id, ok: true, text: 'testing…' })
    try {
      const res = await api.post<TestResultDTO>(`/api/storage/destinations/${d.id}/test`, {})
      setTestMsg({ id: d.id, ok: res.ok, text: res.ok ? 'Connection OK' : `Failed: ${res.error ?? 'unknown'}` })
    } catch (err) {
      setTestMsg({ id: d.id, ok: false, text: err instanceof ApiError ? err.message : 'test failed' })
    }
  }

  // ... JSX: section "Storage Destinations" berisi:
  //   header (ikon Cloud) + tombol <Plus> Add Destination
  //   daftar kartu per destination: nama, endpoint, bucket/prefix, accessKey,
  //     badge secretSet (KeyRound), tombol Test/Edit/Delete per kartu,
  //     testMsg render di bawah kartu yang dites
  //   empty state bila list kosong: "No S3 destinations yet."
  //   <DestinationModal open={showAdd} ... /> dan <DestinationModal open={editing !== undefined} dest={editing} ... />
  //   section CLI Tools lama TIDAK berubah
}
```

Implementasi JSX kartu bebas mengikuti gaya kartu di halaman Databases (border-slate-800, rounded-2xl). Yang wajib: panggilan API persis seperti di atas dan tombol hapus pakai `confirm()` sebelum `remove.mutate(d.id)`.

- [ ] **Step 3: Commit**

```bash
git add web/src/modals/DestinationModal.tsx web/src/pages/Settings.tsx
git commit -m "feat(web): manage multiple S3 destinations from settings"
```

---

### Task 3: DumpModal pilih destination + Databases page

**Files:**
- Modify: `web/src/modals/DumpModal.tsx`, `web/src/pages/Databases.tsx`

- [ ] **Step 1: `DumpModal.tsx`** — ganti prop `storageConfigured: boolean` menjadi fetch internal:

```tsx
import { useQuery } from '@tanstack/react-query'
import { api } from '../lib/api'
import type { StorageDestinationDTO } from '../lib/types'
```

Props: buang `storageConfigured`. State: `const [destId, setDestId] = useState('')`. Query:

```tsx
  const dests = useQuery({
    queryKey: ['storage-destinations'],
    queryFn: () => api.get<StorageDestinationDTO[]>('/api/storage/destinations'),
    enabled: open,
  })
```

Submit: `api.post(..., { label, destId })`.

Field "Target Destination Storage": render grid kartu — Local (`destId=''`, HardDrive, "Save on server disk") + satu kartu per destination (Cloud, subteks `bucket` atau endpoint). Kartu destination aktif style indigo seperti radio lama. Bila `dests.data` kosong: teks kecil "No S3 destinations configured — add one in Settings."

- [ ] **Step 2: `Databases.tsx`** — hapus query `storage` (`/api/settings/storage`) dan prop `storageConfigured={...}` pada `<DumpModal>`; hapus import `StorageSettingsDTO`.

- [ ] **Step 3: Commit**

```bash
git add web/src/modals/DumpModal.tsx web/src/pages/Databases.tsx
git commit -m "feat(web): dump modal selects a storage destination"
```

---

### Task 4: Dumps page + build verifikasi

**Files:**
- Modify: `web/src/pages/Dumps.tsx`

- [ ] **Step 1: Edit**

- Kolom Storage: `d.storage` → `d.destName`; ikon: `d.destId === '' ? <HardDrive/> : <Cloud/>`.
- Stat "S3 Offsite Copies": `allDumps.filter((d) => d.destId !== '').length`.
- Filter storage: opsi "All Storage" (`''`), "Local" (`'local'`), lalu satu opsi per destination dari query `['storage-destinations']` (value `d.id`, label `d.name`); logika:

```tsx
(!storageFilter || (storageFilter === 'local' ? d.destId === '' : d.destId === storageFilter))
```

- [ ] **Step 2: Build & typecheck**

Run: `npm --prefix web run build`
Expected: lulus tanpa error TS.

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/Dumps.tsx
git commit -m "feat(web): dumps page shows and filters by destination"
```

---

## Verification Chunk 2

```bash
npm --prefix web run build
go build ./...
```

Lalu jalankan app (`make dev`) dan cek manual: tambah destination (test koneksi), dump ke destination, dump tampil dengan nama destination di halaman Dumps, restore dari dump tersebut ke database lain masih jalan. Lanjut Chunk 3 untuk e2e otomatis.
