import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { api, ApiError } from '../lib/api'
import type { DatabaseDTO, Engine, TestResultDTO } from '../lib/types'
import { Button, Field, Input, Modal, Select, Spinner } from '../ui'

interface Props {
  open: boolean
  onClose: () => void
  db?: DatabaseDTO
  onSaved: () => void
}

interface FormState {
  name: string
  engine: Engine
  host: string
  port: string
  dbName: string
  username: string
  password: string
  options: string
}

const emptyForm: FormState = {
  name: '',
  engine: 'postgres',
  host: '',
  port: '5432',
  dbName: '',
  username: '',
  password: '',
  options: '',
}

export default function DatabaseModal({ open, onClose, db, onSaved }: Props) {
  const [form, setForm] = useState<FormState>(emptyForm)
  const [error, setError] = useState('')
  const [testMsg, setTestMsg] = useState('')
  const [testing, setTesting] = useState(false)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    setError('')
    setTestMsg('')
    if (db) {
      setForm({
        name: db.name,
        engine: db.engine,
        host: db.host,
        port: String(db.port),
        dbName: db.dbName,
        username: db.username,
        password: '',
        options: db.options === '{}' ? '' : db.options,
      })
    } else {
      setForm(emptyForm)
    }
  }, [open, db])

  function set<K extends keyof FormState>(key: K, value: string) {
    setForm((f) => ({ ...f, [key]: value }))
  }

  async function onTest() {
    setTesting(true)
    setTestMsg('')
    try {
      const payload = { ...form, port: Number(form.port) }
      const res = await api.post<TestResultDTO>('/api/databases/test', payload)
      setTestMsg(res.ok ? 'Connection OK' : `Failed: ${res.error ?? 'unknown'}`)
    } catch (err) {
      setTestMsg(err instanceof ApiError ? err.message : 'test failed')
    } finally {
      setTesting(false)
    }
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setSaving(true)
    try {
      const payload = { ...form, port: Number(form.port) }
      if (db) await api.put(`/api/databases/${db.id}`, payload)
      else await api.post('/api/databases', payload)
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'save failed')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal open={open} onClose={onClose} title={db ? `Edit ${db.name}` : 'Add database'}>
      <form onSubmit={onSubmit} className="space-y-3">
        <div className="grid grid-cols-2 gap-3">
          <Field label="Name">
            <Input value={form.name} onChange={(e) => set('name', e.target.value)} required />
          </Field>
          <Field label="Engine">
            <Select
              value={form.engine}
              onChange={(e) => {
                const engine = e.target.value as Engine
                set('engine', engine)
                set('port', engine === 'postgres' ? '5432' : '27017')
              }}
            >
              <option value="postgres">PostgreSQL</option>
              <option value="mongodb">MongoDB</option>
            </Select>
          </Field>
          <Field label="Host">
            <Input value={form.host} onChange={(e) => set('host', e.target.value)} required />
          </Field>
          <Field label="Port">
            <Input
              value={form.port}
              onChange={(e) => set('port', e.target.value)}
              type="number"
              min={1}
              max={65535}
              required
            />
          </Field>
          <Field label="Database name">
            <Input value={form.dbName} onChange={(e) => set('dbName', e.target.value)} required />
          </Field>
          <Field label="Username">
            <Input value={form.username} onChange={(e) => set('username', e.target.value)} required />
          </Field>
        </div>
        <Field label={db ? 'Password (leave empty to keep)' : 'Password'}>
          <Input type="password" value={form.password} onChange={(e) => set('password', e.target.value)} required={!db} />
        </Field>
        <Field label="Options (JSON, optional — e.g. sslmode / authSource)">
          <Input value={form.options} onChange={(e) => set('options', e.target.value)} placeholder="{}" />
        </Field>
        {error && <p className="text-sm text-red-600">{error}</p>}
        {testMsg && (
          <p className={`text-sm ${testMsg === 'Connection OK' ? 'text-green-600' : 'text-red-600'}`}>{testMsg}</p>
        )}
        <div className="flex justify-between pt-2">
          <Button type="button" variant="outline" onClick={onTest} disabled={testing}>
            {testing ? <Spinner /> : 'Test connection'}
          </Button>
          <Button type="submit" disabled={saving}>
            {saving ? <Spinner /> : 'Save'}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
