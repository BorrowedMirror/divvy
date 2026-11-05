import { Toast } from "@base-ui/react/toast";
import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import type { IpcError_Serialize } from "../bindings";

type Result<T> = { status: "ok"; data: T } | { status: "error"; error: IpcError_Serialize };

/** Await a fire-and-forget command and surface its `IpcError` as a toast. */
export function useCommand() {
  const toast = Toast.useToastManager();
  const { t } = useTranslation();
  return useCallback(
    async <T>(call: Promise<Result<T>>): Promise<Result<T>> => {
      const res = await call;
      if (res.status === "error") {
        toast.add({ title: t("errors.commandFailed"), description: res.error.message, timeout: 5000 });
      }
      return res;
    },
    [toast, t],
  );
}
