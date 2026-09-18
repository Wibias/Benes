/**
 * Seed order for data surfaces: a session cache is the last known board.
 * A dummy `initialData` (empty live overlay, empty lists) must not replace it.
 */
export function mergeDataSurfaceSeed<T>(cached: T | undefined, initialData: T | undefined): T | undefined {
  return cached !== undefined ? cached : initialData;
}
