import { routeFromHash } from "./route";

/** Root component. Workstream G replaces the placeholder views. */
export function App() {
  const route = routeFromHash(window.location.hash);
  return <main data-route={route}>kalandra · {route}</main>;
}
