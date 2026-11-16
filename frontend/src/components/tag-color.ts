// Static class map — Tailwind cannot generate dynamically-constructed class names.
export const TAG_DOT_CLASSES: Record<string, string> = {
  zinc: "bg-zinc-500",
  blue: "bg-blue-500",
  green: "bg-emerald-500",
  amber: "bg-amber-500",
  red: "bg-red-500",
  violet: "bg-violet-500",
  cyan: "bg-cyan-500",
  pink: "bg-pink-500",
}

export const TAG_COLORS = Object.keys(TAG_DOT_CLASSES)

export function tagDotClass(color: string): string {
  return TAG_DOT_CLASSES[color] ?? TAG_DOT_CLASSES.zinc
}
