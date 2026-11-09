// highlight.js ships types for its main module only; lib/common is the same
// API with fewer languages registered.
declare module "highlight.js/lib/common" {
  import hljs from "highlight.js";
  export default hljs;
}
