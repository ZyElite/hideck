import type { CommandEvent } from '../types/commands'

export const COMMAND_EVENT_PAGE_SIZE = 100
export const PASSIVE_COMMAND_EVENT_LIMIT = 300

export function mergeCommandEvents(...lists: readonly CommandEvent[][]): CommandEvent[] {
  const merged = new Map<number, CommandEvent>()
  for (const list of lists) {
    for (const event of list) merged.set(event.id, event)
  }
  return [...merged.values()].sort((left, right) => left.id - right.id)
}

export function retainLatestCommandEvents(events: CommandEvent[], limit: number) {
  if (events.length <= limit) return { events, dropped: false }
  return { events: events.slice(events.length - limit), dropped: true }
}
