package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

type PoolData struct {
	Name     string    `json:"name"`
	Datetime time.Time `json:"datetime"`
	Utility  int       `json:"utility"`
}

type ChartData struct {
	Labels []string `json:"labels"`
	Datasets []struct {
		Label string `json:"label"`
		Data  []int  `json:"data"`
		BorderColor string `json:"borderColor"`
		BackgroundColor string `json:"backgroundColor"`
	} `json:"datasets"`
}

func startWebServer() {
	// Create templates directory
	http.HandleFunc("/", serveHome)
	http.HandleFunc("/api/pools", getPools)
	http.HandleFunc("/api/data", getHistoricalData)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static/"))))

	fmt.Println("Starting web server on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	
	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>SWM Pool Utility Monitor</title>
    <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
    <style>
        body {
            font-family: Arial, sans-serif;
            margin: 0;
            padding: 20px;
            background-color: #f5f5f5;
        }
        .container {
            max-width: 1200px;
            margin: 0 auto;
            background: white;
            padding: 20px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        h1 {
            color: #333;
            text-align: center;
            margin-bottom: 30px;
        }
        .controls {
            display: flex;
            gap: 20px;
            margin-bottom: 30px;
            flex-wrap: wrap;
        }
        .control-group {
            flex: 1;
            min-width: 200px;
        }
        label {
            display: block;
            margin-bottom: 5px;
            font-weight: bold;
        }
        select, input {
            width: 100%;
            padding: 8px;
            border: 1px solid #ddd;
            border-radius: 4px;
        }
        button {
            background-color: #007bff;
            color: white;
            padding: 10px 20px;
            border: none;
            border-radius: 4px;
            cursor: pointer;
            font-size: 16px;
        }
        button:hover {
            background-color: #0056b3;
        }
        .chart-container {
            position: relative;
            height: 400px;
            margin-top: 20px;
        }
        .stats {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 20px;
            margin-top: 30px;
        }
        .stat-card {
            background: #f8f9fa;
            padding: 15px;
            border-radius: 6px;
            text-align: center;
        }
        .stat-value {
            font-size: 24px;
            font-weight: bold;
            color: #007bff;
        }
        .stat-label {
            font-size: 14px;
            color: #666;
            margin-top: 5px;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>SWM Pool Utility Monitor</h1>
        
        <div class="controls">
            <div class="control-group">
                <label for="poolSelect">Select Pool:</label>
                <select id="poolSelect" multiple>
                    <option value="">All Pools</option>
                </select>
            </div>
            <div class="control-group">
                <label for="startDate">Start Date:</label>
                <input type="date" id="startDate">
            </div>
            <div class="control-group">
                <label for="endDate">End Date:</label>
                <input type="date" id="endDate">
            </div>
            <div class="control-group">
                <label>&nbsp;</label>
                <button onclick="loadData()">Load Data</button>
            </div>
        </div>

        <div class="chart-container">
            <canvas id="utilityChart"></canvas>
        </div>

        <div class="stats" id="stats">
            <div class="stat-card">
                <div class="stat-value" id="avgUtility">-</div>
                <div class="stat-label">Average Utility</div>
            </div>
            <div class="stat-card">
                <div class="stat-value" id="maxUtility">-</div>
                <div class="stat-label">Peak Utility</div>
            </div>
            <div class="stat-card">
                <div class="stat-value" id="minUtility">-</div>
                <div class="stat-label">Minimum Utility</div>
            </div>
            <div class="stat-card">
                <div class="stat-value" id="dataPoints">-</div>
                <div class="stat-label">Data Points</div>
            </div>
        </div>
    </div>

    <script>
        let chart = null;

        async function loadPools() {
            try {
                const response = await fetch('/api/pools');
                const pools = await response.json();
                const select = document.getElementById('poolSelect');
                
                pools.forEach(pool => {
                    const option = document.createElement('option');
                    option.value = pool;
                    option.textContent = pool;
                    select.appendChild(option);
                });
            } catch (error) {
                console.error('Error loading pools:', error);
            }
        }

        async function loadData() {
            const poolSelect = document.getElementById('poolSelect');
            const selectedPools = Array.from(poolSelect.selectedOptions).map(option => option.value);
            const startDate = document.getElementById('startDate').value;
            const endDate = document.getElementById('endDate').value;

            try {
                const params = new URLSearchParams();
                if (selectedPools.length > 0 && !selectedPools.includes('')) {
                    params.append('pools', selectedPools.join(','));
                }
                if (startDate) params.append('start', startDate);
                if (endDate) params.append('end', endDate);

                const response = await fetch(`/api/data?${params}`);
                const data = await response.json();
                
                updateChart(data);
                updateStats(data);
            } catch (error) {
                console.error('Error loading data:', error);
            }
        }

        function updateChart(data) {
            const ctx = document.getElementById('utilityChart').getContext('2d');
            
            if (chart) {
                chart.destroy();
            }

            const colors = ['#FF6384', '#36A2EB', '#FFCE56', '#4BC0C0', '#9966FF'];

            chart = new Chart(ctx, {
                type: 'line',
                data: data,
                options: {
                    responsive: true,
                    maintainAspectRatio: false,
                    plugins: {
                        title: { display: true, text: 'Pool Utility Over Time' },
                        legend: { display: true, position: 'top' }
                    },
                    scales: {
                        y: { beginAtZero: true, max: 100, title: { display: true, text: 'Utility (%)' } },
                        x: { title: { display: true, text: 'Time' } }
                    }
                }
            });
        }

        function updateStats(data) {
            if (!data.datasets || data.datasets.length === 0) {
                ['avgUtility', 'maxUtility', 'minUtility', 'dataPoints'].forEach(id => {
                    document.getElementById(id).textContent = '-';
                });
                return;
            }

            let allValues = [];
            let totalPoints = 0;

            data.datasets.forEach(dataset => {
                allValues = allValues.concat(dataset.data);
                totalPoints += dataset.data.length;
            });

            if (allValues.length === 0) {
                ['avgUtility', 'maxUtility', 'minUtility'].forEach(id => {
                    document.getElementById(id).textContent = '-';
                });
                document.getElementById('dataPoints').textContent = '0';
                return;
            }

            const avg = Math.round(allValues.reduce((a, b) => a + b, 0) / allValues.length);
            const max = Math.max(...allValues);
            const min = Math.min(...allValues);

            document.getElementById('avgUtility').textContent = avg + '%';
            document.getElementById('maxUtility').textContent = max + '%';
            document.getElementById('minUtility').textContent = min + '%';
            document.getElementById('dataPoints').textContent = totalPoints;
        }

        // Initialize
        document.addEventListener('DOMContentLoaded', function() {
            loadPools();
            
            const endDate = new Date();
            const startDate = new Date();
            startDate.setDate(endDate.getDate() - 7);
            
            document.getElementById('startDate').value = startDate.toISOString().split('T')[0];
            document.getElementById('endDate').value = endDate.toISOString().split('T')[0];
            
            loadData();
        });
    </script>
</body>
</html>`

	fmt.Fprintf(w, html)
}

func getPools(w http.ResponseWriter, r *http.Request) {
	db_all, err := show_db_content()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	pools := make(map[string]bool)
	for _, entry := range db_all {
		if entry.name != "" {
			pools[entry.name] = true
		}
	}

	var poolList []string
	for pool := range pools {
		poolList = append(poolList, pool)
	}
	sort.Strings(poolList)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(poolList)
}

func getHistoricalData(w http.ResponseWriter, r *http.Request) {
	db_all, err := show_db_content()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Parse query parameters
	selectedPools := r.URL.Query().Get("pools")
	startDate := r.URL.Query().Get("start")
	endDate := r.URL.Query().Get("end")

	var poolFilter []string
	if selectedPools != "" {
		poolFilter = strings.Split(selectedPools, ",")
	}

	// Filter data
	filteredData := make(map[string][]PoolData)
	for _, entry := range db_all {
		// Pool filter
		if len(poolFilter) > 0 {
			found := false
			for _, pool := range poolFilter {
				if entry.name == pool {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Date filter
		entryTime, err := time.Parse("2006-01-02 15:04:05", entry.dtime)
		if err != nil {
			continue
		}

		if startDate != "" {
			start, err := time.Parse("2006-01-02", startDate)
			if err == nil && entryTime.Before(start) {
				continue
			}
		}

		if endDate != "" {
			end, err := time.Parse("2006-01-02", endDate)
			if err == nil {
				endOfDay := time.Date(end.Year(), end.Month(), end.Day(), 23, 59, 59, 0, end.Location())
				if entryTime.After(endOfDay) {
					continue
				}
			}
		}

		filteredData[entry.name] = append(filteredData[entry.name], PoolData{
			Name:     entry.name,
			Datetime: entryTime,
			Utility:  entry.utility,
		})
	}

	// Convert to chart format
	chartData := ChartData{
		Labels:  []string{},
		Datasets: []struct {
			Label           string `json:"label"`
			Data            []int  `json:"data"`
			BorderColor     string `json:"borderColor"`
			BackgroundColor string `json:"backgroundColor"`
		}{},
	}

	// Get all unique timestamps
	timestamps := make(map[string]bool)
	for _, data := range filteredData {
		for _, point := range data {
			timestamps[point.Datetime.Format("2006-01-02 15:04")] = true
		}
	}

	var sortedTimestamps []string
	for ts := range timestamps {
		sortedTimestamps = append(sortedTimestamps, ts)
	}
	sort.Strings(sortedTimestamps)
	chartData.Labels = sortedTimestamps

	// Create datasets for each pool
	colors := []string{
		"#FF6384", "#36A2EB", "#FFCE56", "#4BC0C0", "#9966FF",
		"#FF9F40", "#FF6384", "#C9CBCF", "#4BC0C0", "#FF6384",
	}

	poolIndex := 0
	for poolName, data := range filteredData {
		// Create data point map for this pool
		dataMap := make(map[string]int)
		for _, point := range data {
			dataMap[point.Datetime.Format("2006-01-02 15:04")] = point.Utility
		}

		// Fill data array
		var poolData []int
		for _, ts := range sortedTimestamps {
			if value, exists := dataMap[ts]; exists {
				poolData = append(poolData, value)
			} else {
				poolData = append(poolData, 0)
			}
		}

		color := colors[poolIndex%len(colors)]
		chartData.Datasets = append(chartData.Datasets, struct {
			Label           string `json:"label"`
			Data            []int  `json:"data"`
			BorderColor     string `json:"borderColor"`
			BackgroundColor string `json:"backgroundColor"`
		}{
			Label:           poolName,
			Data:            poolData,
			BorderColor:     color,
			BackgroundColor: color + "33",
		})
		poolIndex++
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chartData)
}