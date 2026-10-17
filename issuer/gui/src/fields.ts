import { escape } from './layout.js'

/** One field of a credential type's form, as the issuer API describes it. */
export interface FieldInfo {
  key: string
  label: string
  kind: 'text' | 'email' | 'date' | 'choice'
  required: boolean
  options?: string[]
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
export const typeFields = (fields: FieldInfo[], v: Values = {}): string =>
  fields
    .map((f) => {
      const required = f.required ? ' required' : ''
      const name = ` name="${escape(f.key)}"`
      const value = attr('value', v[f.key])
      if (f.kind === 'choice') {
        const options = (f.options ?? []).map((o) => `<option value="${escape(o)}"${o === v[f.key] ? ' selected' : ''}>${escape(o)}</option>`).join('')
        return `\n  <label>${escape(f.label)} <select${name}${required}><option value=""></option>${options}</select></label>`
      }
      if (f.kind === 'date') {
        return `\n  <label>${escape(f.label)} <input${name} placeholder="dd-mm-jjjj" pattern="${datePattern}" title="dd-mm-jjjj" inputmode="numeric"${value}${required}></label>`
      }
      return `\n  <label>${escape(f.label)} <input${name}${f.kind === 'email' ? ' type="email"' : ''}${value}${required}></label>`
    })
    .join('')
