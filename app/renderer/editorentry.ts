// The entry for the editor's own bundle. Monaco is 642 modules and the
// window loads it only when a file is opened, so it is built apart.
export { ask, edits, focus, mount, mountDiff, onJump, saved, selected } from "./editor";
export type { Asking, Half, Where } from "./editor";
