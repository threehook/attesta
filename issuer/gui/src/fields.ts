import { escape } from './layout.js'

/** One field of a credential type's form, as the issuer API describes it. */
export interface FieldInfo {
  key: string
  label: string
  kind: 'text' | 'email' | 'date' | 'choice' | 'number'
  required: boolean
  options?: string[]
  /** For a number: bounds as decimal texts, and how many decimals it has (0 or none for a whole number). */
  min?: string
  max?: string
  decimals?: number
}

/** A credential type the issuer gives besides Employee, as the API describes it. */
export interface TypeInfo {
  type: string
  path: string
  title: string
  description: string
  fields: FieldInfo[]
}

/** What a form starts with: nothing for a new credential, the recorded data for a correction. */
export type Values = Record<string, string>

export const datePattern = '\\d{2}-\\d{2}-\\d{4}'

const attr = (name: string, value: string | undefined) => (value === undefined ? '' : ` ${name}="${escape(value)}"`)

/** yyyy-mm-dd, as the issuer stores a date, as the dd-mm-jjjj the forms take. */
export const asFormDate = (value: string | undefined) => (value && /^\d{4}-\d{2}-\d{2}$/.test(value) ? value.split('-').reverse().join('-') : value)

/** The inputs of a type's form. The preview in the editor uses this too, so it looks like the real form. */
export const typeFields = (fields: FieldInfo[], v: Values = {}, readonly: string[] = []): string =>
  fields
    .map((f) => {
      const required = (f.required ? ' required' : '') + (readonly.includes(f.key) ? ' readonly' : '')
      const name = ` name="${escape(f.key)}"`
      const value = attr('value', v[f.key])
      if (f.kind === 'choice') {
        const options = (f.options ?? []).map((o) => `<option value="${escape(o)}"${o === v[f.key] ? ' selected' : ''}>${escape(o)}</option>`).join('')
        return `\n  <label>${escape(f.label)} <select${name}${required}><option value=""></option>${options}</select></label>`
      }
      if (f.kind === 'number') {
        const places = Math.max(0, Math.min(6, Math.trunc(f.decimals ?? 0)))
        const step = places === 0 ? '1' : `0.${'0'.repeat(places - 1)}1`
        return `\n  <label>${escape(f.label)} <input${name} type="number" step="${step}"${attr('min', f.min?.replace(',', '.') || undefined)}${attr('max', f.max?.replace(',', '.') || undefined)}${value}${required}></label>`
      }
      if (f.kind === 'date') {
        return `\n  <label>${escape(f.label)} <input${name} placeholder="dd-mm-jjjj" pattern="${datePattern}" title="dd-mm-jjjj" inputmode="numeric"${value}${required}></label>`
      }
      return `\n  <label>${escape(f.label)} <input${name}${f.kind === 'email' ? ' type="email"' : ''}${value}${required}></label>`
    })
    .join('')
