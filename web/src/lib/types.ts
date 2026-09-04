export type Engine = 'postgres' | 'mongodb'

export interface DatabaseDTO {
  id: string
  name: string
  engine: Engine
  host: string
  port: number
  dbName: string
  username: string
  options: string
  lastTestAt: string | null
  lastTestOk: boolean | null
  hasActiveJob: boolean
  toolsMissing: string[]
}

export type DumpStatus = 'pending' | 'ready' | 'uploaded' | 'failed'

export interface DumpDTO {
  id: string
  databaseId: string
  databaseName: string
  engine: Engine
  label: string
  destId: string
  destName: string
  status: DumpStatus
  sizeBytes: number
  sourceDb: string
  createdBy: string
  createdAt: string
}

export type JobStatus = 'pending' | 'running' | 'success' | 'failed' | 'cancelled'

export interface JobDTO {
  id: string
  type: 'dump' | 'restore'
  databaseId: string
  databaseName: string
  dumpId: string
  dumpLabel: string
  status: JobStatus
  error: string
  logPath: string
  createdAt: string
  startedAt: string | null
  finishedAt: string | null
  logTail?: string
}

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

export interface ToolsDTO {
  postgres: string[]
  mongodb: string[]
}

export interface TestResultDTO {
  ok: boolean
  error?: string
}
