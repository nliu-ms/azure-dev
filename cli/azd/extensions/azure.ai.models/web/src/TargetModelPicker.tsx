import { useState } from "react";
import { Badge, Button } from "@fluentui/react-components";
import { targetModelKey, type TargetModelChoice } from "./targetModels";

type Props = {
  choices: TargetModelChoice[];
  selected: TargetModelChoice | null;
  suggested: TargetModelChoice | null;
  onChange: (target: TargetModelChoice) => void;
};

export function TargetModelPicker({ choices, selected, suggested, onChange }: Props) {
  const [customName, setCustomName] = useState("");
  const [customFormat, setCustomFormat] = useState("OpenAI");
  const isSuggested = selected && suggested && targetModelKey(selected) === targetModelKey(suggested);
  return (
    <div className="target-model-picker">
      <div className="target-model-picker-heading">
        <strong>Choose Target model</strong>
        {selected && <Badge appearance="tint" color="informative">{isSuggested ? "Suggested default" : "Your choice"}</Badge>}
      </div>
      <label>
        <span>Target model</span>
        <select
          aria-label="Target model"
          value={selected ? targetModelKey(selected) : ""}
          onChange={(event) => {
            const target = choices.find((choice) => targetModelKey(choice) === event.target.value);
            if (target) onChange(target);
          }}
        >
          <option value="" disabled>Select a model</option>
          {choices.map((choice) => (
            <option key={targetModelKey(choice)} value={targetModelKey(choice)}>
              {choice.modelName} · {choice.modelFormat}
              {suggested && targetModelKey(choice) === targetModelKey(suggested) ? " — suggested" : ""}
            </option>
          ))}
        </select>
      </label>
      <p>The suggestion is a starting point, not a restriction. Other listed choices come from discovered deployments.</p>
      <details className="custom-target-model">
        <summary>Enter another model</summary>
        <form onSubmit={(event) => {
          event.preventDefault();
          if (customName.trim() && customFormat.trim()) {
            onChange({ modelName: customName.trim(), modelFormat: customFormat.trim() });
          }
        }}>
          <label>
            <span>Model name</span>
            <input
              aria-label="Other model name"
              value={customName}
              onChange={(event) => setCustomName(event.target.value)}
              placeholder="Model name from the Azure catalog"
              maxLength={256}
              required
            />
          </label>
          <label>
            <span>Model format</span>
            <input
              aria-label="Other model format"
              value={customFormat}
              onChange={(event) => setCustomFormat(event.target.value)}
              placeholder="For example, OpenAI"
              maxLength={128}
              required
            />
          </label>
          <Button type="submit" disabled={!customName.trim() || !customFormat.trim()}>Use this model</Button>
        </form>
        <p>Apply the choice before continuing. Assess checks its metadata or regional deployment options; entering a name does not guarantee availability.</p>
      </details>
    </div>
  );
}
