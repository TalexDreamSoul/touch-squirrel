import * as echarts from "echarts/core";
import {
  BarChart,
  HeatmapChart,
  LineChart,
  PieChart,
  ScatterChart,
} from "echarts/charts";
import {
  BrushComponent,
  CalendarComponent,
  DatasetComponent,
  GridComponent,
  LegendComponent,
  TitleComponent,
  ToolboxComponent,
  TooltipComponent,
  VisualMapComponent,
} from "echarts/components";
import { CanvasRenderer } from "echarts/renderers";

/**
 * 集中注册本项目用到的 echarts 模块（tree-shaking 友好）。
 * kumo 的 Chart / TimeseriesChart 不自带 echarts，统一从这里拿实例传入。
 */
echarts.use([
  LineChart,
  BarChart,
  PieChart,
  HeatmapChart,
  ScatterChart,
  GridComponent,
  TooltipComponent,
  LegendComponent,
  VisualMapComponent,
  CalendarComponent,
  BrushComponent,
  ToolboxComponent,
  TitleComponent,
  DatasetComponent,
  CanvasRenderer,
]);

export { echarts };
