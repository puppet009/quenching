import { useSyncExternalStore } from "react";
import { serverClockVersion, subscribeServerClock } from "../utils/time";

// Subscribe to calibration, not every clock tick. Static "minutes ago" labels
// must rerender when the initial time request finishes after their data request.
export function useServerClock(): void {
  useSyncExternalStore(subscribeServerClock, serverClockVersion, serverClockVersion);
}
