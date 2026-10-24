// The entry for the editor's own bundle. Monaco is 642 modules and the
// window loads it only when a file is opened, so it is built apart.
export { ask, edits, mount, mountDiff, onJump, saved, selected } from "./editor";
export type { Asking, Where } from "./editor";
