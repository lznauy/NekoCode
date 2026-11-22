// Mirrors protocol.IsSlashCommandInput. Command arguments may contain paths.
export function isSlashCommandInput(input: string): boolean {
  const [token] = input.trim().split(/\p{White_Space}+/u, 1)
  return token.startsWith('/') && !token.includes('/', 1)
}
