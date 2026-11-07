import React, { useMemo, useState } from "react";
import { Badge, Box, Input, Stack, Table, Text } from "@chakra-ui/react";
import { create, fromJson } from "@bufbuild/protobuf";
import { AdoptedSourceSchema, Repo } from "../../../gen/ts/v1/config_pb";
import {
  AddRepoRequestSchema,
  AdoptedSourceMatch,
  PreviewAdoptedSourceRequestSchema,
} from "../../../gen/ts/v1/service_pb";
import { backrestService } from "../../api/client";
import { useConfig } from "../../app/provider";
import { alerts, formatErrorAlert } from "../../components/common/Alerts";
import { EnumSelector } from "../../components/common/EnumSelector";
import { FormModal } from "../../components/common/FormModal";
import { useShowModal } from "../../components/common/ModalManager";
import { RetentionPolicyView } from "../../components/common/RetentionPolicyView";
import {
  ScheduleDefaultsDaily,
  ScheduleFormItem,
} from "../../components/common/ScheduleFormItem";
import { ConfirmButton } from "../../components/common/SpinButton";
import { Button } from "../../components/ui/button";
import { Field } from "../../components/ui/field";
import {
  forgetPolicyEnabled,
  sameSelector,
  withRule,
} from "../../lib/adoption";
import * as m from "../../paraglide/messages";

// Form state uses the protobuf JSON form, like the other config forms, so the
// shared retention and schedule editors can be reused.
interface RuleForm {
  hostname: string;
  path: string;
  retention: any;
  schedule: any;
}

const defaultForm = (hostname: string, path: string): RuleForm => ({
  hostname,
  path,
  retention: { policyKeepLastN: 30 },
  schedule: {
    maxFrequencyDays: 1,
    clock: "CLOCK_LAST_RUN_TIME",
  },
});

const toRule = (form: RuleForm) =>
  fromJson(
    AdoptedSourceSchema,
    {
      hostname: form.hostname.trim(),
      path: form.path.trim(),
      retention: form.retention,
      schedule: form.schedule,
    },
    { ignoreUnknownFields: true },
  );

/**
 * AdoptSourceModal creates an adopted source rule: a host + path selector with
 * a retention policy and schedule. The rule is only saved after a dry-run
 * preview of the current settings, which lists the matched sources and the
 * snapshots that would be removed.
 */
export const AdoptSourceModal = ({
  repo,
  hostname = "",
  paths = [],
}: {
  repo: Repo;
  hostname?: string;
  // paths of the source the modal was opened from; the user picks one.
  paths?: string[];
}) => {
  const showModal = useShowModal();
  const [, setConfig] = useConfig();
  const [form, setForm] = useState<RuleForm>(() =>
    defaultForm(hostname, paths[0] ?? ""),
  );
  const [preview, setPreview] = useState<{
    key: string;
    matches: AdoptedSourceMatch[];
  } | null>(null);
  const [previewLoading, setPreviewLoading] = useState(false);

  const formKey = JSON.stringify(form);
  const previewCurrent = preview?.key === formKey;
  const forgetConflict = forgetPolicyEnabled(repo);
  const duplicate = repo.adoptedSources.some((r) =>
    sameSelector(r, { hostname: form.hostname.trim(), path: form.path.trim() }),
  );
  const canPreview =
    form.hostname.trim() !== "" && form.path.trim() !== "" && !duplicate;

  const update = (patch: Partial<RuleForm>) =>
    setForm((f) => ({ ...f, ...patch }));

  const runPreview = async () => {
    setPreviewLoading(true);
    try {
      const resp = await backrestService.previewAdoptedSource(
        create(PreviewAdoptedSourceRequestSchema, {
          repoId: repo.id,
          rule: toRule(form),
        }),
      );
      setPreview({ key: formKey, matches: resp.matches });
    } catch (e: any) {
      alerts.error(formatErrorAlert(e, m.adopt_modal_error()));
    } finally {
      setPreviewLoading(false);
    }
  };

  const submit = async () => {
    try {
      const req = create(AddRepoRequestSchema, {
        repo: withRule(repo, toRule(form)),
      });
      setConfig(await backrestService.addRepo(req));
      alerts.success(m.adopt_modal_success());
      showModal(null);
    } catch (e: any) {
      alerts.error(formatErrorAlert(e, m.adopt_modal_error()));
    }
  };

  const pathOptions = useMemo(
    () => paths.map((p) => ({ value: p, label: p })),
    [paths],
  );

  return (
    <FormModal
      isOpen={true}
      onClose={() => showModal(null)}
      title={m.adopt_modal_title()}
      size="4xl"
      footer={
        <>
          <Button variant="ghost" onClick={() => showModal(null)}>
            {m.button_cancel()}
          </Button>
          <Button
            variant="outline"
            onClick={runPreview}
            loading={previewLoading}
            disabled={!canPreview}
            data-testid="adopt-preview"
          >
            {m.adopt_modal_preview()}
          </Button>
          <ConfirmButton
            onClickAsync={submit}
            confirmTitle={m.adopt_modal_confirm()}
            danger
            disabled={!previewCurrent || forgetConflict || duplicate}
            data-testid="adopt-submit"
          >
            {m.adopt_modal_submit()}
          </ConfirmButton>
        </>
      }
    >
      <Stack gap={4}>
        <Box
          borderWidth="1px"
          borderColor="orange.muted"
          bg="orange.subtle"
          borderRadius="md"
          p={3}
        >
          <Text fontSize="sm">{m.adopt_modal_warning()}</Text>
        </Box>
        {forgetConflict && (
          <Text color="fg.error" fontSize="sm" data-testid="adopt-conflict">
            {m.adopt_modal_forget_policy_conflict()}
          </Text>
        )}

        <Field
          label={m.adopt_modal_hostname()}
          helperText={m.adopt_modal_hostname_help()}
        >
          <Input
            value={form.hostname}
            onChange={(e) => update({ hostname: e.target.value })}
            aria-label={m.adopt_modal_hostname()}
          />
        </Field>

        <Field
          label={m.adopt_modal_path()}
          helperText={m.adopt_modal_path_help()}
        >
          {pathOptions.length > 1 ? (
            <EnumSelector
              options={pathOptions}
              value={form.path}
              onChange={(v) => update({ path: v as string })}
              size="sm"
              ariaLabel={m.adopt_modal_path()}
            />
          ) : (
            <Input
              value={form.path}
              onChange={(e) => update({ path: e.target.value })}
              fontFamily="mono"
              aria-label={m.adopt_modal_path()}
            />
          )}
        </Field>

        <Field label={m.adopt_modal_retention()}>
          <RetentionPolicyView
            schedule={form.schedule}
            retention={form.retention}
            onChange={(v) => update({ retention: v })}
          />
        </Field>

        <Field label={m.adopt_modal_schedule()}>
          <ScheduleFormItem
            value={form.schedule}
            onChange={(v) => update({ schedule: v })}
            defaults={ScheduleDefaultsDaily}
          />
        </Field>

        {!previewCurrent && (
          <Text color="fg.muted" fontSize="sm">
            {m.adopt_modal_preview_required()}
          </Text>
        )}
        {preview && previewCurrent && (
          <PreviewTable matches={preview.matches} />
        )}
      </Stack>
    </FormModal>
  );
};

const PreviewTable = ({ matches }: { matches: AdoptedSourceMatch[] }) => {
  if (matches.length === 0) {
    return <Text color="fg.muted">{m.adopt_modal_preview_empty()}</Text>;
  }
  const adopted = matches.filter((x) => !x.refusedReason);
  const toRemove = adopted.reduce((n, x) => n + x.remove.length, 0);
  return (
    <Stack gap={2} data-testid="adopt-preview-table">
      <Text fontSize="sm">
        {m.adopt_modal_preview_summary({
          sources: adopted.length,
          remove: toRemove,
        })}
      </Text>
      <Box overflowX="auto">
        <Table.Root size="sm" variant="outline">
          <Table.Header>
            <Table.Row>
              <Table.ColumnHeader>{m.sources_column_host()}</Table.ColumnHeader>
              <Table.ColumnHeader>
                {m.sources_column_paths()}
              </Table.ColumnHeader>
              <Table.ColumnHeader textAlign="end">
                {m.adopt_modal_preview_keep()}
              </Table.ColumnHeader>
              <Table.ColumnHeader textAlign="end">
                {m.adopt_modal_preview_remove()}
              </Table.ColumnHeader>
              <Table.ColumnHeader>
                {m.adopt_modal_preview_status()}
              </Table.ColumnHeader>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {matches.map((x) => (
              <Table.Row key={x.source?.id}>
                <Table.Cell whiteSpace="nowrap">
                  {x.source?.hostname}
                </Table.Cell>
                <Table.Cell>
                  <Stack gap={0}>
                    {x.source?.paths.map((p) => (
                      <Text key={p} fontFamily="mono" fontSize="xs">
                        {p}
                      </Text>
                    ))}
                  </Stack>
                </Table.Cell>
                <Table.Cell textAlign="end">{x.keep.length}</Table.Cell>
                <Table.Cell textAlign="end">
                  {x.refusedReason ? "–" : x.remove.length}
                </Table.Cell>
                <Table.Cell>
                  {!x.refusedReason ? (
                    <Badge colorPalette="green">
                      {m.adopt_modal_status_adopted()}
                    </Badge>
                  ) : x.skipped ? (
                    <Text fontSize="xs" color="fg.muted">
                      {m.adopt_modal_status_skipped({
                        reason: x.refusedReason,
                      })}
                    </Text>
                  ) : (
                    <Text fontSize="xs" color="fg.error">
                      {m.adopt_modal_status_refused({
                        reason: x.refusedReason,
                      })}
                    </Text>
                  )}
                </Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table.Root>
      </Box>
    </Stack>
  );
};
