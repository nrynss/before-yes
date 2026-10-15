// Test stand-in for $app/paths. Ids are already checked, so encoding is only
// a guard against a path character reaching the address bar.
export function resolve(path: string, params: Record<string, string | number> = {}): string {
  return path.replace(/\[([^\]]+)\]/g, (_, key: string) => encodeURIComponent(String(params[key] ?? '')));
}
