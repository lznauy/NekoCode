def apply(records, event):
    key = event["code"]
    if event["op"] == "delete":
        records.pop(key, None)
    else:
        records[key] = {"rev": event["rev"], "name": event["name"]}
