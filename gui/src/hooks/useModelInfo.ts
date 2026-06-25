import { useEffect, useState } from 'react'
import { safeProviderModel } from '../lib/wails'

export function useModelInfo(): string {
  const [model, setModel] = useState('')

  useEffect(() => {
    safeProviderModel()
      .then((v: string) => {
        if (!v) return
        const [provider, name] = v.split('|')
        if (provider && name) {
          setModel(`${provider} / ${name}`)
        }
      })
      .catch(() => {
        /* ignore */
      })
  }, [])

  return model
}
