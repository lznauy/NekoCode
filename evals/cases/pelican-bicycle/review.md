# 截图复核契约

来源：https://github.com/simonw/pelican-bicycle 。首句采用原始英文题目，后续只加入本地静态矢量交付约束；不是原作者的官方评分方案。

自动验收仅判断工程交付，**不判断是不是鹈鹕、是否真的在骑车、是否好看**。四项视觉指标在自动结果里始终为 not_run，不能把自动 success 当成完整视觉成功；成本比较也要先核实双方视觉质量没有退步。

每次运行从 verification/v1/results/desktop.png 和 mobile.png 复核，另存 visual-review-v1.json，不覆盖原始结果。记录 reviewer_type（human 或 assistant_visual_review）、rubric_version=1、原 trial_id、产品 SHA256、截图路径、每项结论与具体画面依据。不要把模型看图称为人工检查。

- pelican_identity：长喙、喉囊和鸟体是否清楚，还是更像鸭/鹅？
- bicycle_structure：两个轮子、车架、车把、车座、曲柄/踏板是否可辨，连接是否合理？
- riding_pose：身体在座位上方，翅膀与车把、脚与踏板的接触是否可信？是否只是鸟和自行车并排？
- visual_quality：主体是否完整，比例、遮挡、构图与配色是否清楚？在两张截图中有无裁切或消失？

结论使用 passed / failed / uncertain，未知不补通过。视觉复核不是自动金标准，不把少量主观评分当作整体模型能力排名。空白、纯色矩形、仅文字也可能是合法 SVG，不能因此获得视觉通过；自动非空阈值只是基本渲染探针。
