列为 device,value，每台 enabled=1 的设备一行，按设备 id 升序。仅考虑 valid=1 的样本，取 tick 最大的一条，同 tick 取 seq 最大者。没有有效样本的设备仍输出，value 为 NULL。
