package org.dexsqltree.gui;

import javafx.application.Application;
import javafx.application.Platform;
import javafx.beans.property.ReadOnlyStringWrapper;
import javafx.collections.FXCollections;
import javafx.collections.ObservableList;
import javafx.concurrent.Task;
import javafx.geometry.Insets;
import javafx.geometry.Pos;
import javafx.scene.Scene;
import javafx.scene.control.Alert;
import javafx.scene.control.Button;
import javafx.scene.control.ComboBox;
import javafx.scene.control.Label;
import javafx.scene.control.ListCell;
import javafx.scene.control.ListView;
import javafx.scene.control.ProgressIndicator;
import javafx.scene.control.Spinner;
import javafx.scene.control.SplitPane;
import javafx.scene.control.Tab;
import javafx.scene.control.TabPane;
import javafx.scene.control.TableCell;
import javafx.scene.control.TableColumn;
import javafx.scene.control.TableView;
import javafx.scene.control.TextArea;
import javafx.scene.control.TextField;
import javafx.scene.control.Tooltip;
import javafx.scene.control.TreeCell;
import javafx.scene.control.TreeItem;
import javafx.scene.control.TreeView;
import javafx.scene.input.KeyCode;
import javafx.scene.layout.BorderPane;
import javafx.scene.layout.HBox;
import javafx.scene.layout.Priority;
import javafx.scene.layout.Region;
import javafx.scene.layout.VBox;
import javafx.stage.FileChooser;
import javafx.stage.Stage;

import org.dexsqltree.client.CallNode;
import org.dexsqltree.client.CallTree;
import org.dexsqltree.client.DatasetInfo;
import org.dexsqltree.client.DexsqlClient;
import org.dexsqltree.client.DexsqlException;
import org.dexsqltree.client.DexsqlProcess;
import org.dexsqltree.client.Direction;
import org.dexsqltree.client.QueryResult;
import org.dexsqltree.client.TableInfo;

import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Optional;

/**
 * JavaFX desktop shell for dexsql-tree.
 *
 * <p>The UI never parses DEX itself: the Go engine owns parsing and SQL. This
 * process only renders query results and call trees, which keeps the desktop
 * client small and guarantees that the CLI, MCP server and GUI answer
 * identically.
 */
public final class DexsqlTreeApp extends Application {
    private static final String DEFAULT_URL = "http://127.0.0.1:8765";

    private final TextField addressField = new TextField(DEFAULT_URL);
    private final Label datasetLabel = new Label("Not connected");
    private final Label statusLabel = new Label("Ready");
    private final ListView<TableInfo> tableList = new ListView<>();
    private final TextArea sqlArea = new TextArea();
    private final TextArea schemaArea = new TextArea();
    private final TextArea smaliArea = new TextArea();
    private final TableView<ObservableList<String>> resultTable = new TableView<>();
    private final TextField methodField = new TextField();
    private final ComboBox<Direction> directionBox = new ComboBox<>();
    private final Spinner<Integer> depthSpinner = new Spinner<>(1, 16, 3);
    private final TreeView<CallNode> treeView = new TreeView<>();
    private final ProgressIndicator busy = new ProgressIndicator();
    private final Button connectButton = new Button("Connect");
    private final Button openButton = new Button("Open APK");
    private final Button runButton = new Button("Run");
    private final Button treeButton = new Button("Build");

    private DexsqlClient client;
    private DexsqlProcess process;
    private Stage stage;

    @Override
    public void start(Stage stage) {
        this.stage = stage;
        BorderPane root = new BorderPane();
        root.getStyleClass().add("root-pane");
        root.setTop(buildHeader());
        root.setLeft(buildSidebar());
        root.setCenter(buildCenter());
        root.setBottom(buildStatusBar());

        Scene scene = new Scene(root, 1280, 820);
        scene.getStylesheets().add(getClass().getResource("app.css").toExternalForm());
        stage.setTitle("dexsql-tree");
        stage.setScene(scene);
        stage.show();

        handleArguments();
    }

    private Region buildHeader() {
        Label brand = new Label("dexsql-tree");
        brand.getStyleClass().add("brand");
        Label tagline = new Label("SQL and call trees for APK/DEX");
        tagline.getStyleClass().add("tagline");

        addressField.setPrefWidth(280);
        addressField.setOnKeyPressed(e -> {
            if (e.getCode() == KeyCode.ENTER) {
                connect();
            }
        });
        connectButton.getStyleClass().add("primary");
        connectButton.setOnAction(e -> connect());
        openButton.setOnAction(e -> chooseApk());
        openButton.setDisable(true);
        datasetLabel.getStyleClass().add("dataset-label");

        Region spacer = new Region();
        HBox.setHgrow(spacer, Priority.ALWAYS);
        HBox box = new HBox(10, brand, tagline, spacer, addressField, connectButton, openButton, datasetLabel);
        box.setAlignment(Pos.CENTER_LEFT);
        box.getStyleClass().add("header");
        return box;
    }

    private Region buildSidebar() {
        Label title = new Label("Relations");
        title.getStyleClass().add("section-title");
        TextField filter = new TextField();
        filter.setPromptText("Filter tables");
        filter.textProperty().addListener((obs, old, text) -> {
            if (client == null) {
                return;
            }
            String needle = text == null ? "" : text.toLowerCase();
            List<TableInfo> filtered = new ArrayList<>();
            for (TableInfo t : allTables) {
                if (needle.isEmpty() || t.name().toLowerCase().contains(needle)) {
                    filtered.add(t);
                }
            }
            tableList.setItems(FXCollections.observableArrayList(filtered));
        });
        tableList.setPlaceholder(new Label("Connect to browse"));
        tableList.setCellFactory(v -> new ListCell<>() {
            @Override
            protected void updateItem(TableInfo t, boolean empty) {
                super.updateItem(t, empty);
                setText(empty || t == null ? null : t.name());
                setTooltip(empty || t == null ? null : new Tooltip(t.doc()));
            }
        });
        tableList.getSelectionModel().selectedItemProperty().addListener((obs, old, t) -> {
            if (t == null) {
                return;
            }
            StringBuilder sb = new StringBuilder();
            sb.append(t.name()).append('\n');
            if (t.doc() != null) {
                sb.append(t.doc()).append("\n\n");
            }
            sb.append("Columns:\n");
            for (String c : t.cols()) {
                sb.append("  ").append(c).append('\n');
            }
            schemaArea.setText(sb.toString());
            smaliArea.clear();
        });
        schemaArea.setEditable(false);
        schemaArea.setWrapText(true);
        schemaArea.getStyleClass().add("mono");
        schemaArea.setPrefRowCount(14);

        Label hint = new Label("Read-only queries. Up to 1000 rows. Ctrl+Enter runs.");
        hint.setWrapText(true);
        hint.getStyleClass().add("hint");

        Label schemaTitle = new Label("Schema");
        schemaTitle.getStyleClass().add("section-title");
        VBox box = new VBox(8, title, filter, tableList, schemaTitle, schemaArea, hint);
        VBox.setVgrow(tableList, Priority.ALWAYS);
        box.setPadding(new Insets(14));
        box.setPrefWidth(310);
        box.getStyleClass().add("sidebar");
        return box;
    }

    private Region buildCenter() {
        TabPane tabs = new TabPane();
        tabs.setTabClosingPolicy(TabPane.TabClosingPolicy.UNAVAILABLE);
        tabs.getTabs().addAll(buildSqlTab(), buildTreeTab());
        return tabs;
    }

    private Tab buildSqlTab() {
        sqlArea.setWrapText(false);
        sqlArea.getStyleClass().add("mono");
        sqlArea.setText("SELECT descriptor, param_count, access_flags\n"
                + "FROM methods\n"
                + "ORDER BY descriptor\n"
                + "LIMIT 50;");
        sqlArea.setPrefRowCount(7);
        sqlArea.setOnKeyPressed(e -> {
            if (e.isControlDown() && e.getCode() == KeyCode.ENTER) {
                runQuery();
            }
        });
        runButton.getStyleClass().add("primary");
        runButton.setOnAction(e -> runQuery());
        Label hint = new Label("Ctrl+Enter");
        hint.getStyleClass().add("hint");
        HBox bar = new HBox(8, runButton, hint);
        bar.setAlignment(Pos.CENTER_LEFT);

        resultTable.setPlaceholder(new Label("No rows"));
        resultTable.getStyleClass().add("result-table");
        resultTable.setColumnResizePolicy(TableView.UNCONSTRAINED_RESIZE_POLICY);
        VBox box = new VBox(8, sqlArea, bar, resultTable);
        VBox.setVgrow(resultTable, Priority.ALWAYS);
        box.setPadding(new Insets(12));
        return new Tab("SQL Query", box);
    }

    private Tab buildTreeTab() {
        methodField.setPromptText("Lcom/example/Foo;->bar(I)V");
        methodField.setPrefWidth(460);
        directionBox.getItems().setAll(Direction.CALLERS, Direction.CALLEES);
        directionBox.getSelectionModel().select(Direction.CALLERS);
        depthSpinner.setEditable(true);
        treeButton.getStyleClass().add("primary");
        treeButton.setOnAction(e -> buildTree());
        methodField.setOnKeyPressed(e -> {
            if (e.getCode() == KeyCode.ENTER) {
                buildTree();
            }
        });
        HBox bar = new HBox(8, new Label("Method"), methodField, directionBox, new Label("Depth"), depthSpinner, treeButton);
        bar.setAlignment(Pos.CENTER_LEFT);

        treeView.setShowRoot(true);
        treeView.setCellFactory(v -> new CallNodeCell());
        smaliArea.setEditable(false);
        smaliArea.getStyleClass().add("mono");
        smaliArea.setPromptText("Select a node to inspect");

        treeView.getSelectionModel().selectedItemProperty().addListener((obs, old, item) -> {
            if (item == null || item.getValue() == null) {
                return;
            }
            CallNode n = item.getValue();
            smaliArea.setText(n.descriptor()
                    + "\n\nfile_id=" + n.fileId() + "  method_idx=" + n.methodIdx()
                    + "  depth=" + n.depth() + "  calls=" + n.calls() + (n.cycle() ? "  [cycle]" : ""));
        });
        treeView.setOnMouseClicked(e -> {
            if (e.getClickCount() == 2) {
                showSmali();
            }
        });

        SplitPane split = new SplitPane(treeView, smaliArea);
        split.setDividerPositions(0.62);
        VBox box = new VBox(8, bar, split);
        VBox.setVgrow(split, Priority.ALWAYS);
        box.setPadding(new Insets(12));
        return new Tab("Call Tree", box);
    }

    private Region buildStatusBar() {
        busy.setPrefSize(14, 14);
        busy.setVisible(false);
        Label engine = new Label("engine 0.1.0");
        engine.getStyleClass().add("hint");
        Region spacer = new Region();
        HBox.setHgrow(spacer, Priority.ALWAYS);
        Label remote = new Label(DEFAULT_URL);
        remote.getStyleClass().add("hint");
        remote.textProperty().bind(addressField.textProperty());
        HBox bar = new HBox(10, busy, statusLabel, spacer, engine, remote);
        bar.setAlignment(Pos.CENTER_LEFT);
        bar.getStyleClass().add("status-bar");
        return bar;
    }

    private List<TableInfo> allTables = List.of();

    private void handleArguments() {
        List<String> args = getParameters().getRaw();
        String connectUrl = null;
        Path input = null;
        Path binary = null;
        for (int i = 0; i < args.size(); i++) {
            switch (args.get(i)) {
                case "--connect" -> connectUrl = i + 1 < args.size() ? args.get(++i) : null;
                case "--input" -> input = i + 1 < args.size() ? Path.of(args.get(++i)) : null;
                case "--bin" -> binary = i + 1 < args.size() ? Path.of(args.get(++i)) : null;
                default -> input = Path.of(args.get(i));
            }
        }
        if (connectUrl != null) {
            addressField.setText(connectUrl);
            connect();
            return;
        }
        if (input != null) {
            startLocal(input, binary);
            return;
        }
        connect();
    }

    private void startLocal(Path input, Path binary) {
        Path exe = binary;
        if (exe == null) {
            Optional<Path> found = DexsqlProcess.locateBinary();
            if (found.isEmpty()) {
                alert("Engine not found", "No dexsql-tree executable found.\n"
                        + "Pass --bin or set DEXSQL_TREE_BIN.");
                return;
            }
            exe = found.get();
        }
        setBusy(true, "Starting engine...");
        Path finalExe = exe;
        Task<DexsqlProcess> task = new Task<>() {
            @Override
            protected DexsqlProcess call() throws Exception {
                return DexsqlProcess.start(finalExe, input);
            }
        };
        task.setOnSucceeded(e -> {
            process = task.getValue();
            addressField.setText("http://127.0.0.1:" + process.port());
            setBusy(false, "Engine ready");
            connect();
        });
        task.setOnFailed(e -> {
            setBusy(false, "Start failed");
            alert("Start failed", String.valueOf(task.getException()));
        });
        Thread t = new Thread(task, "dexsql-start");
        t.setDaemon(true);
        t.start();
    }

    private void connect() {
        String url = addressField.getText().trim();
        if (url.isEmpty()) {
            return;
        }
        setBusy(true, "Connecting...");
        DexsqlClient candidate = new DexsqlClient(url);
        Task<List<TableInfo>> task = new Task<>() {
            @Override
            protected List<TableInfo> call() {
                if (!candidate.health()) {
                    throw new DexsqlException("Cannot reach " + url);
                }
                return candidate.tables();
            }
        };
        task.setOnSucceeded(e -> {
            client = candidate;
            allTables = task.getValue();
            tableList.setItems(FXCollections.observableArrayList(allTables));
            if (!allTables.isEmpty()) {
                tableList.getSelectionModel().select(0);
            }
            List<DatasetInfo> datasets = client.datasets();
            if (!datasets.isEmpty()) {
                DatasetInfo d = datasets.get(0);
                client.useDataset(d.id());
                datasetLabel.setText(shortName(d.path()) + "  ·  " + plural(d.stats().classes(), "class")
                        + " / " + plural(d.stats().methods(), "method"));
            } else {
                datasetLabel.setText("No dataset loaded");
            }
            openButton.setDisable(false);
            setBusy(false, "Connected");
        });
        task.setOnFailed(e -> {
            setBusy(false, "Connection failed");
            alert("Connection failed", String.valueOf(task.getException()));
        });
        Thread t = new Thread(task, "dexsql-connect");
        t.setDaemon(true);
        t.start();
    }

    private void chooseApk() {
        FileChooser fc = new FileChooser();
        fc.setTitle("Select APK or DEX");
        fc.getExtensionFilters().addAll(
                new FileChooser.ExtensionFilter("Android package", "*.apk", "*.dex", "*.jar", "*.zip"),
                new FileChooser.ExtensionFilter("All files", "*.*"));
        java.io.File f = fc.showOpenDialog(stage);
        if (f != null) {
            loadDataset(f.toPath());
        }
    }

    private void loadDataset(Path path) {
        if (client == null) {
            alert("Not connected", "Connect to an engine first.");
            return;
        }
        setBusy(true, "Parsing " + path.getFileName() + " ...");
        Task<DatasetInfo> task = new Task<>() {
            @Override
            protected DatasetInfo call() {
                return client.openDataset(path);
            }
        };
        task.setOnSucceeded(e -> {
            DatasetInfo d = task.getValue();
            client.useDataset(d.id());
            datasetLabel.setText(shortName(d.path()) + "  ·  " + plural(d.stats().classes(), "class")
                    + " / " + plural(d.stats().methods(), "method"));
            setBusy(false, "Loaded dataset " + d.id());
        });
        task.setOnFailed(e -> {
            setBusy(false, "Load failed");
            alert("Load failed", String.valueOf(task.getException()));
        });
        Thread t = new Thread(task, "dexsql-open");
        t.setDaemon(true);
        t.start();
    }

    private void runQuery() {
        if (client == null) {
            alert("Not connected", "Connect and load an APK first.");
            return;
        }
        String sql = sqlArea.getText().trim();
        if (sql.isEmpty()) {
            return;
        }
        setBusy(true, "Running...");
        Task<QueryResult> task = new Task<>() {
            @Override
            protected QueryResult call() {
                return client.query(sql, 1000);
            }
        };
        task.setOnSucceeded(e -> {
            renderResult(task.getValue());
            setBusy(false, "Query finished");
        });
        task.setOnFailed(e -> {
            setBusy(false, "Query failed");
            alert("Query failed", rootMessage(task.getException()));
        });
        Thread t = new Thread(task, "dexsql-query");
        t.setDaemon(true);
        t.start();
    }

    private void renderResult(QueryResult r) {
        resultTable.getColumns().clear();
        resultTable.getItems().clear();
        for (int i = 0; i < r.columns().size(); i++) {
            final int col = i;
            TableColumn<ObservableList<String>, String> c = new TableColumn<>(r.columns().get(i));
            c.setCellValueFactory(data -> {
                String v = col < data.getValue().size() ? data.getValue().get(col) : null;
                return new ReadOnlyStringWrapper(v == null ? "NULL" : v);
            });
            c.setCellFactory(cv -> new TableCell<>() {
                @Override
                protected void updateItem(String item, boolean empty) {
                    super.updateItem(item, empty);
                    if (empty || item == null) {
                        setText(null);
                        setGraphic(null);
                        return;
                    }
                    setText(item);
                    getStyleClass().remove("null-cell");
                    if ("NULL".equals(item)) {
                        getStyleClass().add("null-cell");
                    }
                }
            });
            c.setPrefWidth(Math.min(420, Math.max(110, r.columns().get(i).length() * 9.5)));
            resultTable.getColumns().add(c);
        }
        for (List<Object> row : r.rows()) {
            ObservableList<String> line = FXCollections.observableArrayList();
            for (Object v : row) {
                line.add(v == null ? null : String.valueOf(v));
            }
            resultTable.getItems().add(line);
        }
        statusLabel.setText(r.rowCount() + " rows · " + r.elapsedMs() + " ms" + (r.truncated() ? " · truncated" : ""));
    }

    private void buildTree() {
        if (client == null) {
            alert("Not connected", "Connect and load an APK first.");
            return;
        }
        String method = methodField.getText().trim();
        if (method.isEmpty()) {
            return;
        }
        Direction dir = directionBox.getValue();
        int depth = depthSpinner.getValue();
        setBusy(true, "Building tree...");
        Task<CallTree> task = new Task<>() {
            @Override
            protected CallTree call() {
                return client.tree(method, dir, depth);
            }
        };
        task.setOnSucceeded(e -> {
            CallTree tree = task.getValue();
            if (tree.root() != null) {
                TreeItem<CallNode> root = toItem(tree.root());
                root.setExpanded(true);
                treeView.setRoot(root);
            }
            setBusy(false, tree.nodeCount() + " nodes · " + tree.edgeCount() + " edges");
        });
        task.setOnFailed(e -> {
            setBusy(false, "Build failed");
            alert("Build failed", rootMessage(task.getException()));
        });
        Thread t = new Thread(task, "dexsql-tree");
        t.setDaemon(true);
        t.start();
    }

    private TreeItem<CallNode> toItem(CallNode node) {
        TreeItem<CallNode> item = new TreeItem<>(node);
        if (node.children() != null) {
            for (CallNode c : node.children()) {
                item.getChildren().add(toItem(c));
            }
        }
        return item;
    }

    private void showSmali() {
        TreeItem<CallNode> item = treeView.getSelectionModel().getSelectedItem();
        if (item == null || item.getValue() == null || client == null) {
            return;
        }
        String desc = item.getValue().descriptor();
        setBusy(true, "Disassembling...");
        Task<String> task = new Task<>() {
            @Override
            protected String call() {
                return client.smali(desc);
            }
        };
        task.setOnSucceeded(e -> {
            setBusy(false, "smali");
            TextArea area = new TextArea(task.getValue());
            area.setEditable(false);
            area.getStyleClass().add("mono");
            area.setPrefRowCount(30);
            area.setPrefColumnCount(100);
            Alert a = new Alert(Alert.AlertType.INFORMATION);
            a.initOwner(stage);
            a.setTitle("smali");
            a.setHeaderText(desc);
            a.getDialogPane().setContent(area);
            a.showAndWait();
        });
        task.setOnFailed(e -> {
            setBusy(false, "Disassembly failed");
            alert("Disassembly failed", rootMessage(task.getException()));
        });
        Thread t = new Thread(task, "dexsql-smali");
        t.setDaemon(true);
        t.start();
    }

    private static String rootMessage(Throwable t) {
        Throwable cur = t;
        while (cur.getCause() != null) {
            cur = cur.getCause();
        }
        return cur.getMessage() == null ? cur.toString() : cur.getMessage();
    }

    private static String plural(int n, String word) {
        return n + " " + word + (n == 1 ? "" : "s");
    }

    private static String shortName(String path) {
        if (path == null) {
            return "";
        }
        int i = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'));
        return i >= 0 ? path.substring(i + 1) : path;
    }

    private void setBusy(boolean on, String message) {
        Platform.runLater(() -> {
            busy.setVisible(on);
            statusLabel.setText(message);
            runButton.setDisable(on);
            treeButton.setDisable(on);
            connectButton.setDisable(on);
            openButton.setDisable(on || client == null);
        });
    }

    private void alert(String title, String message) {
        Platform.runLater(() -> {
            Alert a = new Alert(Alert.AlertType.WARNING);
            a.initOwner(stage);
            a.setTitle(title);
            a.setHeaderText(title);
            a.setContentText(message);
            a.showAndWait();
        });
    }

    @Override
    public void stop() {
        if (process != null) {
            process.close();
        }
    }

    /** Tree cell that shows the short name plus the owner class. */
    private static final class CallNodeCell extends TreeCell<CallNode> {
        @Override
        protected void updateItem(CallNode n, boolean empty) {
            super.updateItem(n, empty);
            getStyleClass().remove("cycle-cell");
            if (empty || n == null) {
                setText(null);
                setGraphic(null);
                return;
            }
            String suffix = n.calls() > 1 ? "  x" + n.calls() : "";
            if (n.depth() == 0) {
                setText(n.descriptor());
            } else {
                setText(n.name() + suffix + "   " + n.className());
            }
            if (n.cycle()) {
                getStyleClass().add("cycle-cell");
                setText(getText() + "   [cycle]");
            }
        }
    }
}
