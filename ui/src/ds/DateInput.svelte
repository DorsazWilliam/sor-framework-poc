<script lang="ts">
  import { DatePicker } from "bits-ui";
  import { parseDate } from "@internationalized/date";
  import CalendarIcon from "~icons/fluent/calendar-16-regular";
  import Prev from "~icons/fluent/chevron-left-16-regular";
  import Next from "~icons/fluent/chevron-right-16-regular";
  import { i18n } from "../labels/i18n.svelte";

  let { value = $bindable(), id }: { value?: string | null; id?: string } = $props();
</script>

<DatePicker.Root
  bind:value={() => (value ? parseDate(value) : undefined), (v) => (value = v ? v.toString() : null)}
  locale="{i18n.lang}-CH"
  weekStartsOn={1}
  granularity="day"
  fixedWeeks
>
  <DatePicker.Input class="input date" {id}>
    {#snippet children({ segments })}
      <span class="date-segments">
        {#each segments as { part, value: v }, i (i)}
          <DatePicker.Segment {part} class="date-seg">{v}</DatePicker.Segment>
        {/each}
      </span>
      <DatePicker.Trigger class="field-btn"><CalendarIcon /></DatePicker.Trigger>
    {/snippet}
  </DatePicker.Input>
  <DatePicker.Portal>
    <DatePicker.Content class="popover" sideOffset={4}>
      <DatePicker.Calendar class="calendar">
        {#snippet children({ months, weekdays })}
          <DatePicker.Header class="cal-header">
            <DatePicker.PrevButton class="field-btn"><Prev /></DatePicker.PrevButton>
            <DatePicker.Heading />
            <DatePicker.NextButton class="field-btn"><Next /></DatePicker.NextButton>
          </DatePicker.Header>
          {#each months as month (month.value.toString())}
            <DatePicker.Grid class="cal-grid">
              <DatePicker.GridHead>
                <DatePicker.GridRow>
                  {#each weekdays as d (d)}<DatePicker.HeadCell>{d.slice(0, 2)}</DatePicker.HeadCell>{/each}
                </DatePicker.GridRow>
              </DatePicker.GridHead>
              <DatePicker.GridBody>
                {#each month.weeks as week, w (w)}
                  <DatePicker.GridRow>
                    {#each week as date (date.toString())}
                      <DatePicker.Cell {date} month={month.value}><DatePicker.Day class="cal-day" /></DatePicker.Cell>
                    {/each}
                  </DatePicker.GridRow>
                {/each}
              </DatePicker.GridBody>
            </DatePicker.Grid>
          {/each}
        {/snippet}
      </DatePicker.Calendar>
    </DatePicker.Content>
  </DatePicker.Portal>
</DatePicker.Root>
