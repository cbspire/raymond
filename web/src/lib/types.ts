export interface Route {
  domain: string;
  target: string;
}
export interface Config {
  version: number;
  dns: { zones: string[]; listen: string };
  proxy: { listen: string; allow_wildcard_coexistence?: boolean };
  routes: Route[];
}
export interface ConfigView {
  config: Config;
  revision: string;
  error?: string;
}
export interface State {
  operation?: Operation;
  enrolled?: boolean;
  version: string;
  platform: string;
  running: boolean;
  saved: ConfigView;
  active: ConfigView;
  pending: boolean;
}
export interface Operation {
  id: string;
  status: string;
  phase?: string;
  error?: string;
  result?: unknown;
}
export type Tab = "services" | "dns" | "logs" | "system";
