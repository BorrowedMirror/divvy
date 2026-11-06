import { Checkbox } from "@base-ui/react/checkbox";
import { NumberField } from "@base-ui/react/number-field";
import { Select } from "@base-ui/react/select";
import { Switch } from "@base-ui/react/switch";
import { Toggle } from "@base-ui/react/toggle";
import { ToggleGroup } from "@base-ui/react/toggle-group";
import { Tooltip } from "@base-ui/react/tooltip";
import type { ButtonHTMLAttributes, ReactNode } from "react";

export function cx(...parts: (string | false | null | undefined)[]): string {
  return parts.filter(Boolean).join(" ");
}

const focusRing =
  "outline-none focus-visible:ring-2 focus-visible:ring-gold focus-visible:ring-offset-1 focus-visible:ring-offset-bg";

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "primary" | "ghost" | "danger" };

export function Button({ variant = "ghost", className, type = "button", ...rest }: ButtonProps) {
  return (
    <button
      type={type}
      className={cx(
        "inline-flex items-center justify-center gap-1.5 rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
        "disabled:cursor-not-allowed disabled:opacity-50",
        variant === "primary" && "bg-gold text-bg hover:bg-gold-bright",
        variant === "ghost" && "border border-line bg-surface-2 text-fg hover:border-gold/60",
        variant === "danger" && "border border-bad/60 bg-transparent text-bad hover:bg-bad/10",
        focusRing,
        className,
      )}
      {...rest}
    />
  );
}

/** Icon-only button with an accessible name and a hover tooltip. */
export function IconButton({
  label,
  children,
  pressed,
  className,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { label: string; pressed?: boolean }) {
  return (
    <Tooltip.Root>
      <Tooltip.Trigger
        render={
          <button
            type="button"
            aria-label={label}
            aria-pressed={pressed}
            className={cx(
              "inline-flex h-7 w-7 items-center justify-center rounded-md text-muted hover:bg-surface-2 hover:text-fg",
              pressed && "bg-gold/15 text-gold",
              focusRing,
              className,
            )}
            {...rest}
          />
        }
      >
        {children}
      </Tooltip.Trigger>
      <Tooltip.Portal>
        <Tooltip.Positioner sideOffset={6}>
          <Tooltip.Popup className="rounded bg-surface-3 px-2 py-1 text-xs text-fg shadow-lg">{label}</Tooltip.Popup>
        </Tooltip.Positioner>
      </Tooltip.Portal>
    </Tooltip.Root>
  );
}

export function CheckboxField({
  checked,
  onChange,
  label,
  hideLabel,
  disabled,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label: ReactNode;
  hideLabel?: boolean;
  disabled?: boolean;
}) {
  return (
    // biome-ignore lint/a11y/noLabelWithoutControl: Base UI's Checkbox.Root inside is the control.
    <label className={cx("inline-flex min-w-0 cursor-pointer items-center gap-2 text-sm", disabled && "opacity-50")}>
      <Checkbox.Root
        checked={checked}
        disabled={disabled}
        onCheckedChange={(v) => onChange(v)}
        className={cx(
          "flex h-4 w-4 shrink-0 items-center justify-center rounded-sm border border-line bg-surface-2",
          "data-[checked]:border-gold data-[checked]:bg-gold",
          focusRing,
        )}
      >
        <Checkbox.Indicator className="text-bg">
          <svg viewBox="0 0 12 12" width="10" height="10" aria-hidden="true">
            <path d="M2 6.5 5 9l5-6" fill="none" stroke="currentColor" strokeWidth="2" />
          </svg>
        </Checkbox.Indicator>
      </Checkbox.Root>
      <span className={cx("min-w-0", hideLabel && "sr-only")}>{label}</span>
    </label>
  );
}

export function SwitchField({
  checked,
  onChange,
  label,
  description,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label: string;
  description?: string;
}) {
  return (
    // biome-ignore lint/a11y/noLabelWithoutControl: Base UI's Switch.Root inside is the control.
    <label className="flex cursor-pointer items-start justify-between gap-4 py-1">
      <span className="flex flex-col">
        <span className="text-sm">{label}</span>
        {description && <span className="text-xs text-muted">{description}</span>}
      </span>
      <Switch.Root
        checked={checked}
        onCheckedChange={(v) => onChange(v)}
        className={cx(
          "relative mt-0.5 inline-flex h-5 w-9 shrink-0 rounded-full border border-line bg-surface-3 transition-colors",
          "data-[checked]:border-gold data-[checked]:bg-gold/80",
          focusRing,
        )}
      >
        <Switch.Thumb className="block h-4 w-4 translate-x-0.5 translate-y-px rounded-full bg-fg transition-transform data-[checked]:translate-x-4" />
      </Switch.Root>
    </label>
  );
}

export type Option<T extends string> = { value: T; label: string };

export function SelectField<T extends string>({
  value,
  onChange,
  options,
  label,
  hideLabel,
  disabled,
  className,
  id,
}: {
  value: T;
  onChange: (v: T) => void;
  options: Option<T>[];
  label: string;
  hideLabel?: boolean;
  disabled?: boolean;
  className?: string;
  /** Lets an external `<label htmlFor>` target the trigger. */
  id?: string;
}) {
  return (
    <Select.Root
      id={id}
      items={options}
      value={value}
      disabled={disabled}
      onValueChange={(v) => {
        if (v !== null) onChange(v);
      }}
    >
      <div className={cx("flex flex-col gap-1", className)}>
        <Select.Label className={cx("text-xs text-muted", hideLabel && "sr-only")}>{label}</Select.Label>
        <Select.Trigger
          className={cx(
            "flex min-w-0 items-center justify-between gap-2 rounded-md border border-line bg-surface-2 px-2.5 py-1.5 text-sm",
            "hover:border-gold/60 data-[disabled]:opacity-50",
            focusRing,
          )}
        >
          <Select.Value className="truncate" />
          <Select.Icon className="text-muted" aria-hidden="true">
            ▾
          </Select.Icon>
        </Select.Trigger>
      </div>
      <Select.Portal>
        <Select.Positioner sideOffset={4} className="z-50" alignItemWithTrigger={false}>
          <Select.Popup className="max-h-72 min-w-(--anchor-width) overflow-auto rounded-md border border-line bg-surface-2 p-1 shadow-xl">
            <Select.List>
              {options.map((o) => (
                <Select.Item
                  key={o.value}
                  value={o.value}
                  className="flex cursor-default items-center justify-between gap-3 rounded px-2 py-1 text-sm outline-none data-[highlighted]:bg-gold/20"
                >
                  <Select.ItemText>{o.label}</Select.ItemText>
                  <Select.ItemIndicator className="text-gold">✓</Select.ItemIndicator>
                </Select.Item>
              ))}
            </Select.List>
          </Select.Popup>
        </Select.Positioner>
      </Select.Portal>
    </Select.Root>
  );
}

export function NumberInput({
  value,
  onChange,
  label,
  min,
  max,
  step = 1,
  placeholder,
  disabled,
  className,
  invalid,
  id,
}: {
  id?: string;
  value: number | null;
  onChange: (v: number | null) => void;
  label: string;
  min?: number;
  max?: number;
  step?: number | "any";
  placeholder?: string;
  disabled?: boolean;
  className?: string;
  invalid?: boolean;
}) {
  return (
    <NumberField.Root
      value={value}
      onValueChange={(v) => onChange(v)}
      min={min}
      max={max}
      step={step}
      disabled={disabled}
      className={className}
      id={id}
    >
      <NumberField.Group>
        <NumberField.Input
          aria-label={label}
          aria-invalid={invalid || undefined}
          placeholder={placeholder}
          className={cx(
            "w-full rounded-md border bg-surface-2 px-2 py-1 text-right text-sm tabular-nums",
            invalid ? "border-bad" : "border-line",
            "disabled:opacity-50",
            focusRing,
          )}
        />
      </NumberField.Group>
    </NumberField.Root>
  );
}

export function Segmented<T extends string>({
  value,
  onChange,
  options,
  label,
}: {
  value: T;
  onChange: (v: T) => void;
  options: Option<T>[];
  label: string;
}) {
  return (
    <ToggleGroup
      aria-label={label}
      value={[value]}
      onValueChange={(v) => {
        const next = v[0];
        if (next) onChange(next as T);
      }}
      className="inline-flex rounded-md border border-line bg-surface-2 p-0.5"
    >
      {options.map((o) => (
        <Toggle
          key={o.value}
          value={o.value}
          className={cx(
            "rounded px-2 py-0.5 text-xs text-muted data-[pressed]:bg-gold data-[pressed]:text-bg",
            focusRing,
          )}
        >
          {o.label}
        </Toggle>
      ))}
    </ToggleGroup>
  );
}

export function Spinner({ label }: { label: string }) {
  return (
    <span role="status" className="inline-flex items-center gap-2 text-sm text-muted">
      <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-gold border-t-transparent" aria-hidden="true" />
      {label}
    </span>
  );
}
