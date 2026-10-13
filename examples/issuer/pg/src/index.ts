import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import pg from 'pg'

export interface DatabaseOptions {
  /** `host` or `host:port`. */
  host: string
  user: string
  password: string
}

/** Connects to the database, creating it and applying the .sql files in `migrations` that have not run yet. */
export async function openDatabase(options: DatabaseOptions, database: string, migrations: string): Promise<pg.Pool> {
  const [host, port] = options.host.split(':')
  const config = { host, port: port ? Number(port) : 5432, user: options.user, password: options.password }
  const admin = new pg.Client({ ...config, database: 'postgres' })
  await admin.connect()
  try {
    // The lock keeps replicas that start together from creating the database at the same moment.
    await admin.query('SELECT pg_advisory_lock(7010)')
    const exists = await admin.query('SELECT 1 FROM pg_database WHERE datname = $1', [database])
    if (exists.rowCount === 0) await admin.query(`CREATE DATABASE ${pg.escapeIdentifier(database)}`)
  } finally {
    await admin.end()
  }
  const pool = new pg.Pool({ ...config, database })
  try {
    await migrate(pool, migrations)
  } catch (error) {
    await pool.end()
    throw error
  }
  return pool
}

// Applies the migrations in name order, each once. The lock keeps replicas that start together from racing.
async function migrate(pool: pg.Pool, migrations: string) {
  const client = await pool.connect()
  try {
    await client.query('SELECT pg_advisory_lock(7011)')
    await client.query('CREATE TABLE IF NOT EXISTS schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())')
    const done = new Set((await client.query<{ name: string }>('SELECT name FROM schema_migrations')).rows.map((r) => r.name))
    for (const name of readdirSync(migrations).filter((f) => f.endsWith('.sql')).sort()) {
      if (done.has(name)) continue
      await client.query('BEGIN')
      try {
        await client.query(readFileSync(join(migrations, name), 'utf8'))
        await client.query('INSERT INTO schema_migrations (name) VALUES ($1)', [name])
        await client.query('COMMIT')
      } catch (error) {
        await client.query('ROLLBACK')
        throw error
      }
    }
  } finally {
    await client.query('SELECT pg_advisory_unlock(7011)').catch(() => {})
    client.release()
  }
}
