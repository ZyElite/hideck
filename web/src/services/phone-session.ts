const PHONE_CONTROL_KEY = 'vohive_phone_control'

export type SavedPhoneControl = { mediaId: string; lease: string }

export function readPhoneControl(): SavedPhoneControl {
  try {
    const saved = JSON.parse(storage()?.getItem(PHONE_CONTROL_KEY) || '{}') as Partial<SavedPhoneControl>
    return { mediaId: saved.mediaId || '', lease: saved.lease || '' }
  } catch {
    storage()?.removeItem(PHONE_CONTROL_KEY)
    return { mediaId: '', lease: '' }
  }
}

export function savePhoneControl(control: SavedPhoneControl) {
  const session = storage()
  if (!session) return
  if (!control.mediaId && !control.lease) {
    session.removeItem(PHONE_CONTROL_KEY)
    return
  }
  session.setItem(PHONE_CONTROL_KEY, JSON.stringify(control))
}

function storage() {
  try {
    return typeof sessionStorage === 'undefined' ? null : sessionStorage
  } catch {
    return null
  }
}
